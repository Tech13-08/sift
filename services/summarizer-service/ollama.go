package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBodyRunes   = 2500
	maxPromptRunes = 28000
)

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Stream   bool                `json:"stream"`
	Format   any                 `json:"format,omitempty"`
	Options  map[string]any      `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatResponse struct {
	Message ollamaChatMessage `json:"message"`
	Error   string            `json:"error,omitempty"`
}

type oneEmailCategory struct {
	Keep    bool   `json:"keep"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Why     string `json:"why"`
}

type userRuleParse struct {
	Instructions []string `json:"instructions"`
	Mutes        []string `json:"mutes"`
	Unmutes      []string `json:"unmutes"`
	Removes      []string `json:"removes"`
	Reply        string   `json:"reply"`
}

type greetingDraft struct {
	Greeting string `json:"greeting"`
}

var oneEmailJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "keep": {"type": "boolean"},
    "title": {"type": "string"},
    "summary": {"type": "string"},
    "why": {"type": "string"}
  },
  "required": ["keep", "why"]
}`)

var greetingJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "greeting": {"type": "string"}
  },
  "required": ["greeting"]
}`)

var insightAnswerJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "answer": {"type": "string"}
  },
  "required": ["answer"]
}`)

var userRuleJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "instructions": {"type": "array", "items": {"type": "string"}},
    "mutes": {"type": "array", "items": {"type": "string"}},
    "unmutes": {"type": "array", "items": {"type": "string"}},
    "removes": {"type": "array", "items": {"type": "string"}},
    "reply": {"type": "string"}
  },
  "required": ["reply"]
}`)

const categorizeOnePrompt = `Decide if this one email is important. JSON only.

The BODY is the source of truth. The subject is a headline and can be a copied post title. Never decide from the subject alone.

First ask: is this ABOUT the mailbox owner, or ABOUT someone else?

About the owner: a person or company wrote them; the body talks to them or about their money, their time, their account, or an application they submitted.
About someone else / a share: the body is a post, thread, or story by another person (community name, username/handle, upvotes, comments, "posted in", "shared", or a stranger writing I/me about their own interview or job). Then keep=false even if the subject sounds like it happened to the owner.

keep=true if it is about the owner AND they would want it in a short daily note: a meeting or time to confirm, a person waiting on a reply, their money or account, or a real outcome of something they applied for (interview with them, offer, rejection). Personal mail like that can stay even if it is not urgent. A reply in a conversation the owner already wrote in is always keep=true.

When it is a toss-up between a share and a personal letter, keep=false.

User rules can conflict. A watch/keep rule that fits this email wins over a skip rule about a broader class. Only keep=false for the skip when the body is clearly that subclass (a blast or product pitch), not the thing they asked to watch.

keep=false for newsletters, sales, job/hackathon recommendations they did not apply to, and daily monitors with no action required.

If keep=false, leave title and summary empty.

If keep=true:
title and summary speak to the reader as you/your. Never I, me, my, mine, I'm.
title: short accurate line — the real company/person and what happened. Not the mailer/processor. Not a copied post title.
summary: 1–3 sentences from the BODY only. Do not invent.

Always set why: one short clause naming the real reason (e.g. "payment confirmation", "product ad not a specific opening", "newsletter blast", "matches watched early-career role").`

const greetingPrompt = `JSON only: {"greeting":"..."}. One short greeting to the reader (Good morning / Good afternoon / Good evening / Happy Friday). Not Hey/Hi/Hello. Not I/me/my. Not a caption.`

const cullImportantPrompt = `These were flagged. JSON only: {"keep":[1,3]}

keep = 1-based indexes that are about the mailbox owner (their meeting, money, account, application outcome, or watched job match). Drop only clear someone-else posts/threads or generic marketing broadcasts.
Never drop: a reply in their thread, a payment/finance update, an application confirmation/rejection/offer, a login/security alert, or anything marked as matching a watch rule.`

const interpretRulePrompt = `The user is teaching Sift what mail matters. Convert their message into standing instructions for a later importance picker.

JSON only:
{"instructions":["specific rule that names the mail, e.g. skip job-site product ads; keep specific job alerts"],"mutes":["brand or sender name"],"unmutes":["substring"],"removes":["substring of a rule to delete"],"reply":"short confirmation to the user"}

instructions are appended to the keep/skip prompt. They must say WHAT mail — copy the user's distinction. Never write "this kind of mail" or "this kind of email".
mutes hide matching senders in code. Use a brand/person name only (Extern, HireFT). Never mute a category (job hunting, job site, newsletters, advertising).
removes/unmutes delete existing rules whose text matches. If they are not changing rules, use empty arrays and reply briefly. If they want rules listed, instructions=[] and reply="list". If they ask how to use the bot (help, what commands), reply="help". Setting job alerts or saying which jobs to watch is a rule, not help: put the criteria in instructions (e.g. treat full-time US early-career/new-grad job mail as important). If they are asking whether mail arrived (did I get any Hyundai emails today), instructions=[] and reply="insight". "I don't need / don't want / skip mail from X" is a rule, not a search: named brands go in mutes; any nuance (ads vs real alerts) goes in instructions. Never reply="insight" or "help" for a preference they want saved. If they want a rule gone, put a short needle in removes and do not add a new instruction.`

func categorizeOneEmail(ctx context.Context, msg ingestedMessage, sys string, claimed bool) (messageFacts, error) {
	if sys == "" {
		sys = categorizeOnePrompt
	}
	user := formatEmailForCategorize(msg, claimed)
	raw, err := ollamaJSON(ctx, sys, user, oneEmailJSONSchema, 280, 0.1)
	if err != nil {
		return messageFacts{}, err
	}
	var cat oneEmailCategory
	if err := json.Unmarshal([]byte(extractJSON(raw)), &cat); err != nil {
		return messageFacts{}, fmt.Errorf("categorize json: %w", err)
	}
	why := strings.TrimSpace(cat.Why)
	f := messageFacts{
		Title:   strings.TrimSpace(cat.Title),
		Summary: strings.TrimSpace(cat.Summary),
	}
	if cat.Keep {
		f.Kind = kindNotice
		if f.Title == "" {
			f.Title = collapseSpace(msg.subject)
		}
		f.Title = deFirstPerson(f.Title)
		f.Summary = deFirstPerson(f.Summary)
		f.Outcome = decisionOutcome("keep", "qwen", why)
	} else {
		f.Kind = kindPromo
		f.Title = ""
		f.Summary = ""
		f.Outcome = decisionOutcome("skip", "qwen", why)
	}
	return f, nil
}

func draftGreeting(ctx context.Context, localNow time.Time) (string, error) {
	user := "Local time is " + localNow.Format("Monday 3:04 PM") + "."
	raw, err := ollamaJSON(ctx, greetingPrompt, user, greetingJSONSchema, 40, 0.7)
	if err != nil {
		return "", err
	}
	var g greetingDraft
	if err := json.Unmarshal([]byte(extractJSON(raw)), &g); err != nil {
		return "", err
	}
	g.Greeting = strings.TrimSpace(g.Greeting)
	if !looksLikeGreeting(g.Greeting) {
		return fallbackGreeting(localNow), nil
	}
	return deFirstPerson(g.Greeting), nil
}

type cullKeep struct {
	Keep []int `json:"keep"`
}

var cullKeepJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "keep": {"type": "array", "items": {"type": "integer"}}
  },
  "required": ["keep"]
}`)

func cullUnimportant(ctx context.Context, kept []messageFacts) []messageFacts {
	if len(kept) <= 1 {
		return kept
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d flagged emails. Drop ones that are about someone else. Keep personal items.\n", len(kept))
	for i, f := range kept {
		title := strings.TrimSpace(f.Title)
		if title == "" {
			title = compileLine(f)
		}
		from := strings.TrimSpace(f.From)
		sum := strings.TrimSpace(f.Summary)
		if utf8.RuneCountInString(sum) > 180 {
			sum = strings.TrimSpace(string([]rune(sum)[:180])) + "…"
		}
		fmt.Fprintf(&b, "%d. from=%s\n   title=%s\n   summary=%s\n", i+1, from, title, sum)
	}
	raw, err := ollamaJSON(ctx, cullImportantPrompt, b.String(), cullKeepJSONSchema, 80, 0.1)
	if err != nil {
		log.Printf("cull: %v", err)
		return kept
	}
	var picked cullKeep
	if err := json.Unmarshal([]byte(extractJSON(raw)), &picked); err != nil {
		log.Printf("cull json: %v", err)
		return kept
	}
	chosen := map[int]bool{}
	for _, n := range picked.Keep {
		if n >= 1 && n <= len(kept) {
			chosen[n-1] = true
		}
	}
	out := selectAfterCull(kept, picked.Keep)
	for i, f := range kept {
		if chosen[i] {
			continue
		}
		if mustKeepFact(f) {
			logDecide("keep", "cull-restore", f.From, f.Title, "protected item restored after cull")
			continue
		}
		logDecide("skip", "cull", f.From, f.Title, "dropped as broadcast/share")
	}
	log.Printf("cull kept %d/%d titles=%q", len(out), len(kept), factTitles(out))
	return out
}

func selectAfterCull(kept []messageFacts, picks []int) []messageFacts {
	chosen := map[int]bool{}
	for _, n := range picks {
		if n >= 1 && n <= len(kept) {
			chosen[n-1] = true
		}
	}
	var out []messageFacts
	for i, f := range kept {
		if chosen[i] || mustKeepFact(f) {
			out = append(out, f)
		}
	}
	return out
}

func mustKeepFact(f messageFacts) bool {
	if f.ReplyToMe || f.Claimed {
		return true
	}
	blob := strings.ToLower(f.Title + " " + f.Summary + " " + f.What + " " + f.Outcome)
	return looksClosedApplication(blob) || looksTimeAsk(blob) || looksMoneyEvent(blob) ||
		containsAny(blob, "declined", "rejected", "rejection", "not moving you", "offer letter",
			"application was sent", "application sent", "applied to", "finance update", "financial",
			"sign-in", "sign in", "login", "authentication", "account confirmation")
}

func factTitles(facts []messageFacts) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		t := strings.TrimSpace(f.Title)
		if t == "" {
			t = compileLine(f)
		}
		out = append(out, t)
	}
	return out
}

func interpretUserRule(ctx context.Context, userText string) (userRuleParse, error) {
	raw, err := ollamaJSON(ctx, interpretRulePrompt, userText, userRuleJSONSchema, 400, 0.2)
	if err != nil {
		return userRuleParse{}, err
	}
	var parsed userRuleParse
	if err := json.Unmarshal([]byte(extractJSON(raw)), &parsed); err != nil {
		return userRuleParse{}, fmt.Errorf("rule json: %w", err)
	}
	parsed.Reply = strings.TrimSpace(parsed.Reply)
	return parsed, nil
}

func ollamaJSON(ctx context.Context, sys, user string, format any, numPredict int, temperature float64) (string, error) {
	base := strings.TrimRight(os.Getenv("OLLAMA_URL"), "/")
	if base == "" {
		return "", fmt.Errorf("OLLAMA_URL unset")
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "qwen2.5:14b"
	}
	reqBody := ollamaChatRequest{
		Model: model,
		Messages: []ollamaChatMessage{
			{Role: "system", Content: sys},
			{Role: "user", Content: user},
		},
		Stream: false,
		Format: format,
		Options: map[string]any{
			"temperature": temperature,
			"num_predict": numPredict,
		},
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, base+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var parsed ollamaChatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("ollama decode: %w", err)
	}
	if parsed.Error != "" {
		return "", fmt.Errorf("ollama: %s", parsed.Error)
	}
	out := strings.TrimSpace(parsed.Message.Content)
	if out == "" {
		return "", fmt.Errorf("ollama empty")
	}
	return out, nil
}

func formatEmailForCategorize(msg ingestedMessage, claimed bool) string {
	body := strings.TrimSpace(msg.body)
	if body == "" {
		body = "(no body stored — do not invent what happened from the subject)"
	} else {
		body = clipBody(body, maxBodyRunes)
	}
	hint := "Read the body before the subject. Decide whether this happened to the mailbox owner or is a post/thread about someone else."
	if msg.replyToMe {
		hint = "This email is a reply in a conversation the mailbox owner already wrote in. keep=true. Summarize what they said."
	} else if claimed {
		hint = "This email matches a watch/keep rule. keep=true unless the body is clearly the skipped subclass from a skip rule (a product pitch or blast), not the watched item."
	}
	return fmt.Sprintf("%s\n\nFrom: %s\nSubject: %s\n\nBody:\n%s", hint, msg.from, msg.subject, body)
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		if end := strings.LastIndex(s, "]"); end > 0 {
			return s[:end+1]
		}
		return s
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func clipBody(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:max])) + "\n[truncated]"
}

var asidePat = regexp.MustCompile(`(?is)\(aside:.*?\)`)

var firstPersonSubs = []struct {
	pat  *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\bI'm\b`), "you are"},
	{regexp.MustCompile(`(?i)\bI am\b`), "you are"},
	{regexp.MustCompile(`(?i)\bI was\b`), "you were"},
	{regexp.MustCompile(`(?i)\bI've\b`), "you have"},
	{regexp.MustCompile(`(?i)\bI'll\b`), "you will"},
	{regexp.MustCompile(`(?i)\bI'd\b`), "you would"},
	{regexp.MustCompile(`(?i)\bI\b`), "you"},
	{regexp.MustCompile(`(?i)\bme\b`), "you"},
	{regexp.MustCompile(`(?i)\bmy\b`), "your"},
	{regexp.MustCompile(`(?i)\bmine\b`), "yours"},
}

func deFirstPerson(s string) string {
	s = strings.TrimSpace(s)
	for _, sub := range firstPersonSubs {
		s = sub.pat.ReplaceAllString(s, sub.repl)
	}
	return strings.TrimSpace(s)
}

// youVoiceInsight keeps Sift as the searcher ("I didn't see") but rewrites
// mail events so they happen to the owner ("invited you", not "invited me").
var insightOwnerSubs = []struct {
	pat  *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)\bI was invited\b`), "you were invited"},
	{regexp.MustCompile(`(?i)\bI got invited\b`), "you got invited"},
	{regexp.MustCompile(`(?i)\bI'm invited\b`), "you're invited"},
	{regexp.MustCompile(`(?i)\bI am invited\b`), "you are invited"},
	{regexp.MustCompile(`(?i)\bme\b`), "you"},
	{regexp.MustCompile(`(?i)\bmy\b`), "your"},
	{regexp.MustCompile(`(?i)\bmine\b`), "yours"},
}

func youVoiceInsight(s string) string {
	s = strings.TrimSpace(s)
	for _, sub := range insightOwnerSubs {
		s = sub.pat.ReplaceAllString(s, sub.repl)
	}
	return strings.TrimSpace(s)
}

func cleanDraft(s string) string {
	s = asidePat.ReplaceAllString(s, "")
	var kept []string
	for _, para := range strings.Split(s, "\n") {
		t := strings.TrimSpace(para)
		low := strings.ToLower(t)
		if t == "" {
			if len(kept) > 0 && kept[len(kept)-1] != "" {
				kept = append(kept, "")
			}
			continue
		}
		if strings.HasPrefix(low, "i'm ") || strings.HasPrefix(low, "i am ") || strings.HasPrefix(low, "i feel") {
			continue
		}
		kept = append(kept, t)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

var weakOpeners = []string{
	"hey", "hi", "hello", "hey there", "hi there", "hello there",
	"yo", "what's up", "whats up", "howdy", "hiya", "hey!",
}

func firstOpenLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(strings.TrimLeft(s, "*_# "))
}

func weakOpener(s string) bool {
	line := firstOpenLine(s)
	if line == "" {
		return false
	}
	head := line
	if i := strings.IndexAny(line, ",.—–—!?:"); i >= 0 {
		head = strings.TrimSpace(line[:i])
	}
	head = strings.ToLower(strings.Trim(head, "*_ "))
	for _, w := range weakOpeners {
		if head == w {
			return true
		}
	}
	return false
}

func looksLikeGreeting(s string) bool {
	line := firstOpenLine(s)
	if line == "" || weakOpener(s) {
		return false
	}
	low := strings.ToLower(line)
	if containsAny(low, " here", "wrapping up", "workweek", "winding down the") {
		return false
	}
	if containsAny(low, "good morning", "good afternoon", "good evening", "good night",
		"happy monday", "happy tuesday", "happy wednesday", "happy thursday",
		"happy friday", "happy saturday", "happy sunday", "happy weekend",
		"hope you", "hope your", "hope tonight", "hope the night") {
		return true
	}
	return timeSalutation(low)
}

func timeSalutation(low string) bool {
	for _, w := range []string{"morning", "afternoon", "evening", "night"} {
		if low == w || strings.HasPrefix(low, w+",") || strings.HasPrefix(low, w+".") ||
			strings.HasPrefix(low, w+" —") || strings.HasPrefix(low, w+"—") || strings.HasPrefix(low, w+" -") {
			return true
		}
	}
	return false
}

func fallbackLine(msg ingestedMessage) string {
	from := strings.TrimSpace(msg.from)
	subject := strings.TrimSpace(msg.subject)
	if from == "" {
		from = "(unknown sender)"
	}
	if subject == "" {
		subject = "(no subject)"
	}
	return from + " — " + subject
}
