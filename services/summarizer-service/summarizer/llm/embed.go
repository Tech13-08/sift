package llm

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"sift/summarizer-service/summarizer/model"
)

const embedDims = 768

var MailVectorsEnabled bool

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Input  string `json:"input,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Embedding  []float64   `json:"embedding"`
	Error      string      `json:"error,omitempty"`
}

func EnsureVectorSchema(db *sql.DB) {
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		log.Printf("pgvector unavailable; insight RAG off: %v", err)
		return
	}
	if _, err := db.Exec(`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS embedding vector(768)`); err != nil {
		log.Printf("embedding column: %v", err)
		return
	}
	if _, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS ingested_messages_embedding_idx
		ON ingested_messages USING hnsw (embedding vector_cosine_ops)
	`); err != nil {
		log.Printf("embedding index: %v", err)
	}
	MailVectorsEnabled = true
	log.Print("insight RAG ready (pgvector + ollama embed)")
}

func embedModel() string {
	if m := strings.TrimSpace(os.Getenv("OLLAMA_EMBED_MODEL")); m != "" {
		return m
	}
	return "nomic-embed-text"
}

func StartEmbedBackfill(ctx context.Context, db *sql.DB) {
	if !MailVectorsEnabled {
		return
	}
	run := func() {
		n, err := EmbedPendingMail(ctx, db, 12)
		if err != nil && ctx.Err() == nil {
			log.Printf("embed backfill: %v", err)
			return
		}
		if n > 0 {
			log.Printf("embed backfill wrote %d", n)
		}
	}
	run()
	t := time.NewTicker(45 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

func EmbedPendingMail(ctx context.Context, db *sql.DB, limit int) (int, error) {
	if db == nil || !MailVectorsEnabled {
		return 0, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, COALESCE(from_address, ''), COALESCE(subject, ''),
		       COALESCE(NULLIF(btrim(body_text), ''), snippet, '')
		FROM ingested_messages
		WHERE embedding IS NULL
		ORDER BY ingested_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type row struct {
		ID, From, Subject, Body string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.ID, &r.From, &r.Subject, &r.Body); err != nil {
			return 0, err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, r := range pending {
		vec, err := ollamaEmbed(ctx, embedDocument(r.From, r.Subject, r.Body))
		if err != nil {
			return n, err
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE ingested_messages SET embedding = $2::vector WHERE id = $1::uuid
		`, r.ID, FormatVector(vec)); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func embedDocument(from, subject, body string) string {
	var b strings.Builder
	if from = strings.TrimSpace(from); from != "" {
		fmt.Fprintf(&b, "From: %s\n", from)
	}
	if subject = strings.TrimSpace(subject); subject != "" {
		fmt.Fprintf(&b, "Subject: %s\n", subject)
	}
	body = model.CollapseSpace(body)
	if utf8Count(body) > 1200 {
		body = string([]rune(body)[:1200])
	}
	if body != "" {
		b.WriteString(body)
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = "(empty message)"
	}
	return out
}

func utf8Count(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func ollamaEmbed(ctx context.Context, text string) ([]float64, error) {
	base := strings.TrimRight(os.Getenv("OLLAMA_URL"), "/")
	if base == "" {
		return nil, fmt.Errorf("OLLAMA_URL unset")
	}
	vec, err := ollamaEmbedAt(ctx, base+"/api/embed", ollamaEmbedRequest{Model: embedModel(), Input: text})
	if err == nil {
		return vec, nil
	}
	return ollamaEmbedAt(ctx, base+"/api/embeddings", ollamaEmbedRequest{Model: embedModel(), Prompt: text})
}

func ollamaEmbedAt(ctx context.Context, url string, reqBody ollamaEmbedRequest) ([]float64, error) {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed ollamaEmbedResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	if parsed.Error != "" {
		return nil, fmt.Errorf("ollama embed: %s", parsed.Error)
	}
	vec := parsed.Embedding
	if len(vec) == 0 && len(parsed.Embeddings) > 0 {
		vec = parsed.Embeddings[0]
	}
	if len(vec) == 0 {
		return nil, fmt.Errorf("ollama embed empty")
	}
	return vec, nil
}

func FormatVector(vec []float64) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range vec {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(x, 'f', 6, 64))
	}
	b.WriteByte(']')
	return b.String()
}

func SearchMailByEmbedding(ctx context.Context, db *sql.DB, userID string, q model.MailQuery, question string, limit int) ([]model.InsightHit, error) {
	if db == nil || !MailVectorsEnabled {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	vec, err := ollamaEmbed(ctx, question)
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, COALESCE(from_address, ''), COALESCE(subject, ''), COALESCE(kind, ''),
		       COALESCE(internal_date, ingested_at),
		       COALESCE(NULLIF(btrim(body_text), ''), snippet, '')
		FROM ingested_messages
		WHERE user_id = $1
		  AND embedding IS NOT NULL
		  AND COALESCE(internal_date, ingested_at) >= $2
		  AND COALESCE(internal_date, ingested_at) < $3
		ORDER BY embedding <=> $4::vector
		LIMIT $5
	`, userID, q.Start, q.End, FormatVector(vec), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []model.InsightHit
	for rows.Next() {
		var h model.InsightHit
		if err := rows.Scan(&h.ID, &h.From, &h.Subject, &h.Kind, &h.When, &h.Snippet); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func MergeInsightHits(base, extra []model.InsightHit) []model.InsightHit {
	seen := map[string]bool{}
	var out []model.InsightHit
	add := func(h model.InsightHit) {
		key := h.ID
		if key == "" {
			key = insightHitKey(h)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, h)
	}
	for _, h := range base {
		add(h)
	}
	for _, h := range extra {
		add(h)
	}
	return out
}

func InsightWantsRAG(text string, q model.MailQuery, hits []model.InsightHit) bool {
	if !MailVectorsEnabled || q.Recap {
		return false
	}
	if LooksSemanticInsight(text) {
		return true
	}
	strong, _ := partitionInsightHits(hits, q.Needle)
	return len(strong) == 0
}

func LooksSemanticInsight(s string) bool {
	low := strings.ToLower(s)
	return model.ContainsAny(low,
		"about", "what happened", "anything", "related to",
		"the car", "the bill", "interview", "rejected", "rejection",
		"meeting", "payment")
}

func insightHitKey(h model.InsightHit) string {
	return h.When.UTC().Format(time.RFC3339Nano) + "\n" + h.From + "\n" + h.Subject
}

func partitionInsightHits(hits []model.InsightHit, needle string) (strong, weak []model.InsightHit) {
	for _, h := range hits {
		if insightMatchStrength(h, needle) > 0 {
			strong = append(strong, h)
		} else {
			weak = append(weak, h)
		}
	}
	return strong, weak
}

func insightMatchStrength(h model.InsightHit, needle string) int {
	if brandToken(h.From, needle) {
		return 2
	}
	if brandToken(h.Subject, needle) {
		return 1
	}
	return 0
}

func brandToken(hay, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return false
	}
	for _, tok := range strings.Fields(strings.ToLower(hay)) {
		tok = strings.Trim(tok, ".,;")
		if tok == needle || strings.HasPrefix(tok, needle+".") {
			return true
		}
	}
	return false
}
