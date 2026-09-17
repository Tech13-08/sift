package insights

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sift/summarizer-service/summarizer/llm"
	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
)

const insightAnswerPrompt = `You are Sift, chatting with the mailbox owner in Discord. Answer their question using ONLY the emails below. JSON only: {"answer":"..."}

Answer the question directly, like a person texting back. If they asked yes/no, lead with that. Say what the mail actually said when the body has it.
2-5 short sentences. Casual.
Talk TO them. Mail events happen to them: "Extern invited you", "Hyundai confirmed your payment". Never "invited me", "I was invited", "your payment to me".
"I" is only for Sift searching ("I didn't see any Extern mail", "I found two").
Do not invent. If a hit is only loosely related, say that - don't treat a passing mention as the real thing.
Do not list timestamps. Do not say "1 email on Saturday". Do not dump a bullet list of subjects; sources are attached separately.`

const insightMatchPrompt = `The user asked about mail from a specific sender/brand. JSON only: {"keep":[1,2]}

keep = 1-based indexes that are actually FROM that brand/person, or whose subject is about that brand as the sender. Drop emails that only mention the word in passing (a Meetup/newsletter/community post that uses the same word).

"empower emails" means Empower the company, not a Meetup line that says empower.`

func looksLikeRecapInsight(s string) bool {
	low := strings.ToLower(s)
	if model.ContainsAny(low, "is important", "as important", "are important") &&
		!model.ContainsAny(low, "any important", "what important", "important email", "important mail", "anything important") {
		return false
	}
	return model.ContainsAny(low,
		"important email", "important mail", "anything important", "any important",
		"what mattered", "what was important", "what did sift", "that mattered",
		"what happened", "what's important", "whats important", "what's in my inbox",
		"whats in my inbox", "worth seeing", "sift keep", "sift kept")
}

func ParseMailQuery(text string, now time.Time, loc *time.Location) model.MailQuery {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	low := strings.ToLower(text)
	start, end, label := localDayRange(now, 0)
	switch {
	case model.ContainsAny(low, "24 hour", "24 hr", "last day", "past day"):
		start = now.Add(-24 * time.Hour)
		end = now.Add(time.Minute)
		label = "the past 24 hours"
	case model.ContainsAny(low, "yesterday"):
		start, end, label = localDayRange(now, -1)
	case model.ContainsAny(low, "this week", "past week", "last week", "last 7"):
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -6)
		end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
		label = "the past week"
	case model.ContainsAny(low, "this month"):
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		end = start.AddDate(0, 1, 0)
		label = start.Format("January 2006")
	}
	needle := insightNeedle(text)
	return model.MailQuery{
		Needle: needle,
		Start:  start,
		End:    end,
		Label:  label,
		Recap:  looksLikeRecapInsight(text) && needle == "",
	}
}

func localDayRange(now time.Time, dayOffset int) (time.Time, time.Time, string) {
	loc := now.Location()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, dayOffset)
	return day, day.AddDate(0, 0, 1), day.Format("Monday, January 2")
}

func insightNeedle(text string) string {
	stop := map[string]bool{
		"did": true, "do": true, "i": true, "get": true, "got": true, "any": true,
		"emails": true, "email": true, "mail": true, "mails": true, "inbox": true,
		"this": true, "that": true, "the": true, "a": true, "an": true, "from": true,
		"about": true, "was": true, "were": true, "there": true, "have": true,
		"received": true, "please": true, "show": true, "me": true, "my": true,
		"you": true, "see": true, "sift": true, "today": true, "yesterday": true,
		"day": true, "week": true, "month": true, "past": true, "last": true,
		"check": true, "if": true, "for": true, "on": true, "in": true, "to": true,
		"what": true, "which": true, "who": true, "when": true, "miss": true,
		"missed": true, "some": true, "something": true, "stuff": true,
		"anything": true, "related": true, "happened": true,
		"important": true, "hours": true, "hour": true, "hrs": true, "24": true,
		"since": true, "recap": true, "digest": true, "kept": true,
		"noteworthy": true, "matter": true, "mattered": true, "notable": true,
		"worth": true, "seeing": true, "keep": true,
	}
	var kept []string
	for _, part := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return unicode.IsSpace(r) || r == '?' || r == ',' || r == '.' || r == '!' || r == '"' || r == '\''
	}) {
		part = strings.Trim(part, ".,;:\"'`")
		if part == "" || stop[part] || len(part) < 2 {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, " ")
}

func AnswerInsight(ctx context.Context, db *sql.DB, u model.DigestUser, text string) (string, error) {
	loc := locationOrUTC(u.Timezone)
	q := ParseMailQuery(text, time.Now(), loc)
	if q.Needle == "" && !q.Recap && !llm.LooksSemanticInsight(text) {
		return "Ask about a sender or name, e.g. did I get any Hyundai emails today?", nil
	}
	var hits []model.InsightHit
	var err error
	if q.Recap {
		hits, err = SearchImportantMail(ctx, db, u, q)
	} else {
		hits, err = SearchIngestedMail(ctx, db, u.ID, q)
	}
	if err != nil {
		return "", err
	}
	if !q.Recap && llm.InsightWantsRAG(text, q, hits) {
		if _, err := llm.EmbedPendingMail(ctx, db, 20); err != nil {
			log.Printf("insight embed: %v", err)
		}
		rag, err := llm.SearchMailByEmbedding(ctx, db, u.ID, q, text, 8)
		if err != nil {
			log.Printf("insight rag: %v", err)
		} else if len(rag) > 0 {
			log.Printf("insight rag added %d candidates needle=%q", len(rag), q.Needle)
			hits = llm.MergeInsightHits(hits, rag)
		}
	}
	if !q.Recap {
		cullNeedle := q.Needle
		if strings.TrimSpace(cullNeedle) == "" {
			cullNeedle = text
		}
		hits = FilterInsightHits(ctx, text, cullNeedle, hits)
	}
	answer, err := DraftInsightAnswer(ctx, text, q, hits)
	if err != nil {
		log.Printf("insight answer: %v", err)
		answer = FallbackInsightAnswer(q, hits)
	}
	return FormatInsight(answer, hits), nil
}

func SearchImportantMail(ctx context.Context, db *sql.DB, u model.DigestUser, q model.MailQuery) ([]model.InsightHit, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, COALESCE(from_address, ''), COALESCE(subject, ''), COALESCE(kind, ''),
		       COALESCE(internal_date, ingested_at),
		       COALESCE(NULLIF(btrim(body_text), ''), snippet, ''),
		       COALESCE(in_reply_to_me, false)
		FROM ingested_messages
		WHERE user_id = $1
		  AND COALESCE(internal_date, ingested_at) >= $2
		  AND COALESCE(internal_date, ingested_at) < $3
		  AND (kind = 'notice' OR COALESCE(in_reply_to_me, false) OR kind IS NULL)
		ORDER BY COALESCE(internal_date, ingested_at) ASC
		LIMIT 60
	`, u.ID, q.Start, q.End)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var raw []model.InsightHit
	for rows.Next() {
		var h model.InsightHit
		var reply bool
		if err := rows.Scan(&h.ID, &h.From, &h.Subject, &h.Kind, &h.When, &h.Snippet, &reply); err != nil {
			return nil, err
		}
		if reply {
			h.Kind = model.KindNotice
		}
		raw = append(raw, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rules, err := loadRules(ctx, db, u.ID)
	if err != nil {
		log.Printf("recap rules: %v", err)
	}
	var hits []model.InsightHit
	for _, h := range raw {
		if KeepRecapHit(h, rules) {
			hits = append(hits, h)
		}
		if len(hits) >= 30 {
			break
		}
	}
	log.Printf("insight recap hits=%d window=%s", len(hits), q.Label)
	return hits, nil
}

func KeepRecapHit(h model.InsightHit, rules []model.MailRule) bool {
	if h.Kind == model.KindNotice {
		return true
	}
	if h.Kind == model.KindPromo {
		return false
	}
	msg := model.IngestedMessage{From: h.From, Subject: h.Subject, Body: h.Snippet}
	if alwaysShows(rules, msg) {
		return true
	}
	f, sure := mail.ExtractFacts(msg)
	return sure && f.Kind == model.KindNotice
}

func SearchIngestedMail(ctx context.Context, db *sql.DB, userID string, q model.MailQuery) ([]model.InsightHit, error) {
	if db == nil {
		return nil, nil
	}
	if strings.TrimSpace(q.Needle) == "" {
		return nil, nil
	}
	like := "%" + strings.ToLower(q.Needle) + "%"
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, COALESCE(from_address, ''), COALESCE(subject, ''), COALESCE(kind, ''),
		       COALESCE(internal_date, ingested_at),
		       COALESCE(NULLIF(btrim(body_text), ''), snippet, '')
		FROM ingested_messages
		WHERE user_id = $1
		  AND COALESCE(internal_date, ingested_at) >= $2
		  AND COALESCE(internal_date, ingested_at) < $3
		  AND (
		    lower(COALESCE(from_address, '')) LIKE $4
		    OR lower(COALESCE(subject, '')) LIKE $4
		    OR lower(COALESCE(body_text, '')) LIKE $4
		    OR lower(COALESCE(fact_who, '')) LIKE $4
		  )
		ORDER BY COALESCE(internal_date, ingested_at) ASC
		LIMIT 20
	`, userID, q.Start, q.End, like)
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

func DraftInsightAnswer(ctx context.Context, question string, q model.MailQuery, hits []model.InsightHit) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\nWindow: %s\n", question, q.Label)
	if q.Recap {
		b.WriteString("They asked what was important. Cover every email below - do not only mention one sender.\n")
	}
	if len(hits) == 0 {
		b.WriteString("No matching emails were found.\n")
	} else {
		fmt.Fprintf(&b, "%d emails:\n", len(hits))
		for i, h := range hits {
			snip := model.CollapseSpace(h.Snippet)
			if utf8.RuneCountInString(snip) > 400 {
				snip = strings.TrimSpace(string([]rune(snip)[:400])) + "…"
			}
			fmt.Fprintf(&b, "%d. from=%s\n   subject=%s\n   body=%s\n", i+1, h.From, h.Subject, snip)
		}
	}
	raw, err := llm.OllamaJSON(ctx, insightAnswerPrompt, b.String(), llm.InsightAnswerJSONSchema, 180, 0.4)
	if err != nil {
		return "", err
	}
	var drafted struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(llm.ExtractJSON(raw)), &drafted); err != nil {
		return "", err
	}
	out := llm.YouVoiceInsight(strings.TrimSpace(drafted.Answer))
	if out == "" {
		return "", fmt.Errorf("empty insight answer")
	}
	return out, nil
}

func FallbackInsightAnswer(q model.MailQuery, hits []model.InsightHit) string {
	if len(hits) == 0 {
		if q.Recap {
			return "I didn't keep anything important in " + q.Label + "."
		}
		who := strings.TrimSpace(q.Needle)
		if who == "" {
			return "I didn't find mail for that in this window."
		}
		return "I didn't see any " + who + " mail in this window."
	}
	if len(hits) == 1 {
		return "There's one that matches - " + InsightSourceLine(hits[0]) + "."
	}
	return fmt.Sprintf("There are %d that match. Sources are below.", len(hits))
}

func FormatInsight(answer string, hits []model.InsightHit) string {
	answer = strings.TrimSpace(answer)
	if len(hits) == 0 {
		return answer
	}
	return answer + "\n\nSources:\n" + FormatInsightSources(hits)
}

const insightSourceCap = 12

func FormatInsightSources(hits []model.InsightHit) string {
	shown := hits
	extra := 0
	if len(hits) > insightSourceCap {
		shown = hits[:insightSourceCap]
		extra = len(hits) - insightSourceCap
	}
	var b strings.Builder
	for i, h := range shown {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("• ")
		b.WriteString(InsightSourceLine(h))
	}
	if extra > 0 {
		fmt.Fprintf(&b, "\n• and %d more", extra)
	}
	return b.String()
}

func InsightSourceLine(h model.InsightHit) string {
	from := strings.TrimSpace(mail.SenderWho(h.From))
	if from == "" {
		from = strings.TrimSpace(h.From)
	}
	if from == "" {
		from = "(unknown sender)"
	}
	subj := strings.TrimSpace(h.Subject)
	if subj == "" {
		subj = "(no subject)"
	}
	return model.ClipRunes(from, 40) + " - " + model.ClipRunes(subj, 90)
}

func FilterInsightHits(ctx context.Context, question, needle string, hits []model.InsightHit) []model.InsightHit {
	if len(hits) == 0 {
		return hits
	}
	_, weak := PartitionInsightHits(hits, needle)
	if len(weak) == 0 {
		return hits
	}
	allowed := map[string]bool{}
	picked, err := CullInsightHits(ctx, question, needle, weak)
	if err != nil {
		log.Printf("insight cull: %v", err)
	} else {
		for _, h := range picked {
			allowed[InsightHitKey(h)] = true
		}
	}
	var out []model.InsightHit
	for _, h := range hits {
		if insightMatchStrength(h, needle) > 0 || allowed[InsightHitKey(h)] {
			out = append(out, h)
		}
	}
	return out
}

func InsightHitKey(h model.InsightHit) string {
	return h.When.UTC().Format(time.RFC3339Nano) + "\n" + h.From + "\n" + h.Subject
}

func PartitionInsightHits(hits []model.InsightHit, needle string) (strong, weak []model.InsightHit) {
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
	for _, tok := range splitBrandTokens(hay) {
		if tok == needle || strings.HasPrefix(tok, needle+".") {
			return true
		}
	}
	return false
}

func splitBrandTokens(s string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		t := strings.ToLower(b.String())
		b.Reset()
		if t != "" {
			out = append(out, t)
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func CullInsightHits(ctx context.Context, question, needle string, hits []model.InsightHit) ([]model.InsightHit, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "User question: %s\nBrand/name they mean: %s\n%d candidate emails. Keep only the ones actually from that brand.\n", question, needle, len(hits))
	for i, h := range hits {
		snip := model.CollapseSpace(h.Snippet)
		if utf8.RuneCountInString(snip) > 180 {
			snip = strings.TrimSpace(string([]rune(snip)[:180])) + "…"
		}
		fmt.Fprintf(&b, "%d. from=%s\n   subject=%s\n   snippet=%s\n", i+1, h.From, h.Subject, snip)
	}
	raw, err := llm.OllamaJSON(ctx, insightMatchPrompt, b.String(), llm.CullKeepJSONSchema, 80, 0.1)
	if err != nil {
		return nil, err
	}
	var picked llm.CullKeep
	if err := json.Unmarshal([]byte(llm.ExtractJSON(raw)), &picked); err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var out []model.InsightHit
	for _, n := range picked.Keep {
		if n < 1 || n > len(hits) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, hits[n-1])
	}
	log.Printf("insight cull kept %d/%d needle=%q", len(out), len(hits), needle)
	return out, nil
}
