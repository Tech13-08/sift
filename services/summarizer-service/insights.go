package main

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
)

type mailQuery struct {
	Needle string
	Start  time.Time
	End    time.Time
	Label  string
	Recap  bool
}

type insightHit struct {
	ID      string
	From    string
	Subject string
	Kind    string
	When    time.Time
	Snippet string
}

const insightAnswerPrompt = `You are Sift, chatting with the mailbox owner in Discord. Answer their question using ONLY the emails below. JSON only: {"answer":"..."}

Answer the question directly, like a person texting back. If they asked yes/no, lead with that. Say what the mail actually said when the body has it.
2-5 short sentences. Casual.
Talk TO them. Mail events happen to them: "Extern invited you", "Hyundai confirmed your payment". Never "invited me", "I was invited", "your payment to me".
"I" is only for Sift searching ("I didn't see any Extern mail", "I found two").
Do not invent. If a hit is only loosely related, say that — don't treat a passing mention as the real thing.
Do not list timestamps. Do not say "1 email on Saturday". Do not dump a bullet list of subjects; sources are attached separately.`

const insightMatchPrompt = `The user asked about mail from a specific sender/brand. JSON only: {"keep":[1,2]}

keep = 1-based indexes that are actually FROM that brand/person, or whose subject is about that brand as the sender. Drop emails that only mention the word in passing (a Meetup/newsletter/community post that uses the same word).

"empower emails" means Empower the company, not a Meetup line that says empower.`

func looksLikeInsight(s string) bool {
	if looksLikeRulePreference(s) {
		return false
	}
	if looksLikeRecapInsight(s) {
		return true
	}
	low := strings.ToLower(s)
	asking := containsAny(low,
		"did i", "do i have", "have i gotten", "have i received",
		"any email", "any mail", "got any", "get any", "have any",
		"was there", "were there",
		"what email", "what mail", "what did i", "what have i",
		"show me", "miss", "check if", "did sift", "anything")
	aboutMail := containsAny(low, "email", "mail", "inbox", "about") || strings.Contains(low, "from ")
	return asking && aboutMail
}

func looksLikeRulePreference(s string) bool {
	return looksLikeSkipPreference(s) || looksLikeKeepPreference(s)
}

func looksLikeSkipPreference(s string) bool {
	low := strings.ToLower(s)
	return containsAny(low,
		"don't need", "do not need", "don't want", "do not want",
		"don't show", "do not show", "don't mention", "do not mention",
		"never show", "never mention", "stop showing", "stop mentioning",
		"mute ", "no more", "skip mail", "ignore mail", "advertising the product",
		"not important", "skip ")
}

func looksLikeKeepPreference(s string) bool {
	low := strings.ToLower(s)
	return containsAny(low,
		"treat as important", "treat mail", "always show", "always mention",
		"is important", "as important", "are important")
}

func ruleGetsColorHint(s string) bool {
	if looksLikeSkipPreference(s) && !looksLikeKeepPreference(s) {
		return false
	}
	return looksLikeKeepPreference(s)
}

func looksLikeRecapInsight(s string) bool {
	low := strings.ToLower(s)
	if containsAny(low, "is important", "as important", "are important") &&
		!containsAny(low, "any important", "what important", "important email", "important mail", "anything important") {
		return false
	}
	return containsAny(low,
		"important email", "important mail", "anything important", "any important",
		"what mattered", "what was important", "what did sift", "that mattered",
		"worth seeing", "sift keep", "sift kept")
}

func parseMailQuery(text string, now time.Time, loc *time.Location) mailQuery {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	low := strings.ToLower(text)
	start, end, label := localDayRange(now, 0)
	switch {
	case containsAny(low, "24 hour", "24 hr", "last day", "past day"):
		start = now.Add(-24 * time.Hour)
		end = now.Add(time.Minute)
		label = "the past 24 hours"
	case containsAny(low, "yesterday"):
		start, end, label = localDayRange(now, -1)
	case containsAny(low, "this week", "past week", "last week", "last 7"):
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -6)
		end = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
		label = "the past week"
	case containsAny(low, "this month"):
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		end = start.AddDate(0, 1, 0)
		label = start.Format("January 2006")
	}
	needle := insightNeedle(text)
	return mailQuery{
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

func answerInsight(ctx context.Context, db *sql.DB, u digestUser, text string) (string, error) {
	loc := locationOrUTC(u.Timezone)
	q := parseMailQuery(text, time.Now(), loc)
	if q.Needle == "" && !q.Recap && !looksSemanticInsight(text) {
		return "Ask about a sender or name, e.g. did I get any Hyundai emails today?", nil
	}
	var hits []insightHit
	var err error
	if q.Recap {
		hits, err = searchImportantMail(ctx, db, u, q)
	} else {
		hits, err = searchIngestedMail(ctx, db, u.ID, q)
	}
	if err != nil {
		return "", err
	}
	if !q.Recap && insightWantsRAG(text, q, hits) {
		if _, err := embedPendingMail(ctx, db, 20); err != nil {
			log.Printf("insight embed: %v", err)
		}
		rag, err := searchMailByEmbedding(ctx, db, u.ID, q, text, 8)
		if err != nil {
			log.Printf("insight rag: %v", err)
		} else if len(rag) > 0 {
			log.Printf("insight rag added %d candidates needle=%q", len(rag), q.Needle)
			hits = mergeInsightHits(hits, rag)
		}
	}
	if !q.Recap {
		cullNeedle := q.Needle
		if strings.TrimSpace(cullNeedle) == "" {
			cullNeedle = text
		}
		hits = filterInsightHits(ctx, text, cullNeedle, hits)
	}
	answer, err := draftInsightAnswer(ctx, text, q, hits)
	if err != nil {
		log.Printf("insight answer: %v", err)
		answer = fallbackInsightAnswer(q, hits)
	}
	return formatInsight(answer, hits), nil
}

func searchImportantMail(ctx context.Context, db *sql.DB, u digestUser, q mailQuery) ([]insightHit, error) {
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
	var raw []insightHit
	for rows.Next() {
		var h insightHit
		var reply bool
		if err := rows.Scan(&h.ID, &h.From, &h.Subject, &h.Kind, &h.When, &h.Snippet, &reply); err != nil {
			return nil, err
		}
		if reply {
			h.Kind = kindNotice
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
	var hits []insightHit
	for _, h := range raw {
		if keepRecapHit(h, rules) {
			hits = append(hits, h)
		}
		if len(hits) >= 30 {
			break
		}
	}
	log.Printf("insight recap hits=%d window=%s", len(hits), q.Label)
	return hits, nil
}

func keepRecapHit(h insightHit, rules []mailRule) bool {
	if h.Kind == kindNotice {
		return true
	}
	if h.Kind == kindPromo {
		return false
	}
	msg := ingestedMessage{from: h.From, subject: h.Subject, body: h.Snippet}
	if alwaysShows(rules, msg) {
		return true
	}
	f, sure := extractFacts(msg)
	return sure && f.Kind == kindNotice
}

func searchIngestedMail(ctx context.Context, db *sql.DB, userID string, q mailQuery) ([]insightHit, error) {
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
	var hits []insightHit
	for rows.Next() {
		var h insightHit
		if err := rows.Scan(&h.ID, &h.From, &h.Subject, &h.Kind, &h.When, &h.Snippet); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func draftInsightAnswer(ctx context.Context, question string, q mailQuery, hits []insightHit) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\nWindow: %s\n", question, q.Label)
	if q.Recap {
		b.WriteString("They asked what was important. Cover every email below — do not only mention one sender.\n")
	}
	if len(hits) == 0 {
		b.WriteString("No matching emails were found.\n")
	} else {
		fmt.Fprintf(&b, "%d emails:\n", len(hits))
		for i, h := range hits {
			snip := collapseSpace(h.Snippet)
			if utf8.RuneCountInString(snip) > 400 {
				snip = strings.TrimSpace(string([]rune(snip)[:400])) + "…"
			}
			fmt.Fprintf(&b, "%d. from=%s\n   subject=%s\n   body=%s\n", i+1, h.From, h.Subject, snip)
		}
	}
	raw, err := ollamaJSON(ctx, insightAnswerPrompt, b.String(), insightAnswerJSONSchema, 180, 0.4)
	if err != nil {
		return "", err
	}
	var drafted struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(extractJSON(raw)), &drafted); err != nil {
		return "", err
	}
	out := youVoiceInsight(strings.TrimSpace(drafted.Answer))
	if out == "" {
		return "", fmt.Errorf("empty insight answer")
	}
	return out, nil
}

func fallbackInsightAnswer(q mailQuery, hits []insightHit) string {
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
		return "There's one that matches — " + insightSourceLine(hits[0]) + "."
	}
	return fmt.Sprintf("There are %d that match. Sources are below.", len(hits))
}

func formatInsight(answer string, hits []insightHit) string {
	answer = strings.TrimSpace(answer)
	if len(hits) == 0 {
		return answer
	}
	return answer + "\n\nSources:\n" + formatInsightSources(hits)
}

func formatInsightSources(hits []insightHit) string {
	var b strings.Builder
	for i, h := range hits {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("• ")
		b.WriteString(insightSourceLine(h))
	}
	return b.String()
}

func insightSourceLine(h insightHit) string {
	from := strings.TrimSpace(senderWho(h.From))
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
	return clipRunes(from, 40) + " — " + clipRunes(subj, 90)
}

func filterInsightHits(ctx context.Context, question, needle string, hits []insightHit) []insightHit {
	if len(hits) == 0 {
		return hits
	}
	_, weak := partitionInsightHits(hits, needle)
	if len(weak) == 0 {
		return hits
	}
	allowed := map[string]bool{}
	picked, err := cullInsightHits(ctx, question, needle, weak)
	if err != nil {
		log.Printf("insight cull: %v", err)
	} else {
		for _, h := range picked {
			allowed[insightHitKey(h)] = true
		}
	}
	var out []insightHit
	for _, h := range hits {
		if insightMatchStrength(h, needle) > 0 || allowed[insightHitKey(h)] {
			out = append(out, h)
		}
	}
	return out
}

func insightHitKey(h insightHit) string {
	return h.When.UTC().Format(time.RFC3339Nano) + "\n" + h.From + "\n" + h.Subject
}

func partitionInsightHits(hits []insightHit, needle string) (strong, weak []insightHit) {
	for _, h := range hits {
		if insightMatchStrength(h, needle) > 0 {
			strong = append(strong, h)
		} else {
			weak = append(weak, h)
		}
	}
	return strong, weak
}

func insightMatchStrength(h insightHit, needle string) int {
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

func cullInsightHits(ctx context.Context, question, needle string, hits []insightHit) ([]insightHit, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "User question: %s\nBrand/name they mean: %s\n%d candidate emails. Keep only the ones actually from that brand.\n", question, needle, len(hits))
	for i, h := range hits {
		snip := collapseSpace(h.Snippet)
		if utf8.RuneCountInString(snip) > 180 {
			snip = strings.TrimSpace(string([]rune(snip)[:180])) + "…"
		}
		fmt.Fprintf(&b, "%d. from=%s\n   subject=%s\n   snippet=%s\n", i+1, h.From, h.Subject, snip)
	}
	raw, err := ollamaJSON(ctx, insightMatchPrompt, b.String(), cullKeepJSONSchema, 80, 0.1)
	if err != nil {
		return nil, err
	}
	var picked cullKeep
	if err := json.Unmarshal([]byte(extractJSON(raw)), &picked); err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	var out []insightHit
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

