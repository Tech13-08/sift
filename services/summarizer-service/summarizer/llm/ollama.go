package llm

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

	"sift/summarizer-service/summarizer/digestlog"
	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
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
	Keep        bool   `json:"keep"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Why         string `json:"why"`
	MatchedRule int    `json:"matched_rule"`
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
    "why": {"type": "string"},
    "matched_rule": {"type": "integer"}
  },
  "required": ["keep", "why", "matched_rule"]
}`)

var greetingJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "greeting": {"type": "string"}
  },
  "required": ["greeting"]
}`)

var InsightAnswerJSONSchema = json.RawMessage(`{
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

const CategorizeOnePrompt = `Decide if this one email is important. JSON only.

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
title: short accurate line - the real company/person and what happened. Not the mailer/processor. Not a copied post title.
summary: 1-3 sentences from the BODY only. Do not invent.

Always set why: one short clause naming the real reason (e.g. "payment confirmation", "product ad not a specific opening", "newsletter blast", "matches watched early-career role").

When User rules list numbered keep rules, set matched_rule to that number only if keep=true AND the BODY clearly fits that rule (real payment/donation/finance activity for a finance rule; early-career job match for the job filter; a real application outcome for an application rule). If unsure, matched_rule=0. Never invent a rule number.`

const greetingPrompt = `JSON only: {"greeting":"..."}. One short greeting to the reader (Good morning / Good afternoon / Good evening / Happy Friday). Not Hey/Hi/Hello. Not I/me/my. Not a caption.`

const cullImportantPrompt = `These were flagged. JSON only: {"keep":[1,3]}

keep = 1-based indexes that are about the mailbox owner (their meeting, money, account, application outcome, or watched job match). Drop only clear someone-else posts/threads or generic marketing broadcasts.
Never drop: a reply in their thread, a payment/finance update, an application confirmation/rejection/offer, a login/security alert, or anything marked as matching a watch rule.`

const interpretRulePrompt = `The user is teaching Sift what mail to hide or keep. Convert their message into standing rules.

JSON only:
{"instructions":["keep/watch nuance that names the mail"],"mutes":["sender brand OR short topic phrase to hide"],"unmutes":["substring"],"removes":["substring of a rule to delete"],"reply":"short confirmation to the user"}

Hide intent is always a mute - any phrasing that means hide (ignore, skip, mute, avoid, don't include, leave out, don't show, not interested, …) is the SAME. Put ONE concrete sender email or domain in mutes when they named a mailbox (e.g. community@extern.com or @extern.com / extern.com). Never put a brand word alone in mutes (extern, Chase, Factor75) - those are not valid mutes. For category hides without an email/domain (tech news, ads, sign-in alerts), put the preference in instructions instead (e.g. skip tech news) and leave mutes empty. Never emit both a mute and an instruction for the same hide.
instructions are only for keep/watch nuance and category skips (e.g. skip job-site product ads; keep specific job alerts). If they say include/keep/always-show mail from brands (LinkedIn, Nextdoor, …), put one instruction naming those. A single message may set BOTH keeps and mutes when they give a real email or domain to mute.
mutes hide matching mail in code by From address - must be a full email or a domain (@extern.com).
removes/unmutes delete existing rules whose text matches. If they are not changing rules, use empty arrays and reply briefly. If they want rules listed, instructions=[] and reply="list". If they ask how to use the bot (help, what commands), reply="help". Setting job alerts or saying which jobs to watch is a rule, not help: put the criteria in instructions (e.g. treat full-time US early-career/new-grad job mail as important). If they are asking whether mail arrived (did I get any Hyundai emails today), instructions=[] and reply="insight". Never reply="insight" or "help" for a preference they want saved - including messages that start with "rule" or use include/ignore/mute/always show. If they want a rule gone, put a short needle in removes and do not add a new instruction.`

func CategorizeOneEmail(ctx context.Context, msg model.IngestedMessage, sys string, claimed bool) (model.MessageFacts, error) {
	if sys == "" {
		sys = CategorizeOnePrompt
	}
	user := FormatEmailForCategorize(msg, claimed)
	raw, err := OllamaJSON(ctx, sys, user, oneEmailJSONSchema, 280, 0.1)
	if err != nil {
		return model.MessageFacts{}, err
	}
	var cat oneEmailCategory
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &cat); err != nil {
		return model.MessageFacts{}, fmt.Errorf("categorize json: %w", err)
	}
	why := strings.TrimSpace(cat.Why)
	matched := cat.MatchedRule
	if matched < 0 {
		matched = 0
	}
	f := model.MessageFacts{
		Title:       strings.TrimSpace(cat.Title),
		Summary:     strings.TrimSpace(cat.Summary),
		MatchedRule: matched,
	}
	if cat.Keep {
		f.Kind = model.KindNotice
		if f.Title == "" {
			f.Title = model.CollapseSpace(msg.Subject)
		}
		f.Title = DeFirstPerson(f.Title)
		f.Summary = DeFirstPerson(f.Summary)
		f.Outcome = mail.DecisionOutcome("keep", "qwen", why)
	} else {
		f.Kind = model.KindPromo
		f.Title = ""
		f.Summary = ""
		f.MatchedRule = 0
		f.Outcome = mail.DecisionOutcome("skip", "qwen", why)
	}
	return f, nil
}

func DraftGreeting(ctx context.Context, localNow time.Time) (string, error) {
	user := "Local time is " + localNow.Format("Monday 3:04 PM") + "."
	raw, err := OllamaJSON(ctx, greetingPrompt, user, greetingJSONSchema, 40, 0.7)
	if err != nil {
		return "", err
	}
	var g greetingDraft
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &g); err != nil {
		return "", err
	}
	g.Greeting = strings.TrimSpace(g.Greeting)
	if !LooksLikeGreeting(g.Greeting) {
		return fallbackGreeting(localNow), nil
	}
	return DeFirstPerson(g.Greeting), nil
}

type CullKeep struct {
	Keep []int `json:"keep"`
}

var CullKeepJSONSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "keep": {"type": "array", "items": {"type": "integer"}}
  },
  "required": ["keep"]
}`)

func CullUnimportant(ctx context.Context, kept []model.MessageFacts) []model.MessageFacts {
	if len(kept) <= 1 {
		return kept
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d flagged emails. Drop ones that are about someone else. Keep personal items.\n", len(kept))
	for i, f := range kept {
		title := strings.TrimSpace(f.Title)
		if title == "" {
			title = mail.CompileLine(f)
		}
		from := strings.TrimSpace(f.From)
		sum := strings.TrimSpace(f.Summary)
		if utf8.RuneCountInString(sum) > 180 {
			sum = strings.TrimSpace(string([]rune(sum)[:180])) + "…"
		}
		fmt.Fprintf(&b, "%d. from=%s\n   title=%s\n   summary=%s\n", i+1, from, title, sum)
	}
	raw, err := OllamaJSON(ctx, cullImportantPrompt, b.String(), CullKeepJSONSchema, 80, 0.1)
	if err != nil {
		log.Printf("cull: %v", err)
		return kept
	}
	var picked CullKeep
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &picked); err != nil {
		log.Printf("cull json: %v", err)
		return kept
	}
	chosen := map[int]bool{}
	for _, n := range picked.Keep {
		if n >= 1 && n <= len(kept) {
			chosen[n-1] = true
		}
	}
	out := SelectAfterCull(kept, picked.Keep)
	for i, f := range kept {
		if chosen[i] {
			continue
		}
		if MustKeepFact(f) {
			digestlog.LogDecide(ctx, "keep", "cull-restore", f.From, f.Title, "protected item restored after cull", f.Summary)
			continue
		}
		digestlog.LogDecide(ctx, "skip", "cull", f.From, f.Title, "dropped as broadcast/share", f.Summary)
	}
	digestlog.Logf(ctx, "cull kept %d/%d titles=%q", len(out), len(kept), factTitles(out))
	return out
}

// Model cull is soft - still keep replies, claimed mail, and money/rejection-ish facts.
func SelectAfterCull(kept []model.MessageFacts, picks []int) []model.MessageFacts {
	chosen := map[int]bool{}
	for _, n := range picks {
		if n >= 1 && n <= len(kept) {
			chosen[n-1] = true
		}
	}
	var out []model.MessageFacts
	for i, f := range kept {
		if chosen[i] || MustKeepFact(f) {
			out = append(out, f)
		}
	}
	return out
}

func MustKeepFact(f model.MessageFacts) bool {
	if f.ReplyToMe || f.Claimed {
		return true
	}
	// MatchedRule is only left set after ConfirmMatchedRule — treat as protected.
	if f.MatchedRule > 0 {
		return true
	}
	if strings.Contains(strings.ToLower(f.Outcome), "skip via instruction") {
		return false
	}
	blob := strings.ToLower(f.Title + " " + f.Summary + " " + f.What + " " + f.Outcome)
	// Do not protect on soft labels like "finance update" / "financial" alone — need real signals.
	return mail.LooksClosedApplication(blob) || mail.LooksTimeAsk(blob) || mail.LooksMoneyEvent(blob) ||
		model.ContainsAny(blob, "declined", "rejected", "rejection", "not moving you", "offer letter",
			"application was sent", "application sent", "applied to",
			"sign-in", "sign in", "login", "authentication", "account confirmation",
			"donation", "donated", "payment confirmation", "payment received")
}

func factTitles(facts []model.MessageFacts) []string {
	out := make([]string, 0, len(facts))
	for _, f := range facts {
		t := strings.TrimSpace(f.Title)
		if t == "" {
			t = mail.CompileLine(f)
		}
		out = append(out, t)
	}
	return out
}

func InterpretUserRule(ctx context.Context, userText string) (model.UserRuleParse, error) {
	raw, err := OllamaJSON(ctx, interpretRulePrompt, userText, userRuleJSONSchema, 400, 0.2)
	if err != nil {
		return model.UserRuleParse{}, err
	}
	var parsed model.UserRuleParse
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &parsed); err != nil {
		return model.UserRuleParse{}, fmt.Errorf("rule json: %w", err)
	}
	parsed.Reply = strings.TrimSpace(parsed.Reply)
	return parsed, nil
}

func OllamaJSON(ctx context.Context, sys, user string, format any, numPredict int, temperature float64) (string, error) {
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

func FormatEmailForCategorize(msg model.IngestedMessage, claimed bool) string {
	body := strings.TrimSpace(msg.Body)
	if body == "" {
		body = "(no body stored - do not invent what happened from the subject)"
	} else {
		body = ClipBody(body, maxBodyRunes)
	}
	hint := "Read the body before the subject. Decide whether this happened to the mailbox owner or is a post/thread about someone else."
	if msg.ReplyToMe {
		hint = "This email is a reply in a conversation the mailbox owner already wrote in. keep=true. Summarize what they said."
	} else if claimed {
		hint = "This email matches a watch/keep rule. keep=true unless the body is clearly the skipped subclass from a skip rule (a product pitch or blast), not the watched item."
	}
	return fmt.Sprintf("%s\n\nFrom: %s\nSubject: %s\n\nBody:\n%s", hint, msg.From, msg.Subject, body)
}

func ExtractJSON(s string) string {
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

func ClipBody(s string, max int) string {
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

func DeFirstPerson(s string) string {
	s = strings.TrimSpace(s)
	for _, sub := range firstPersonSubs {
		s = sub.pat.ReplaceAllString(s, sub.repl)
	}
	return strings.TrimSpace(s)
}

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

// Keep Sift as "I" for searching, but mail events address the owner ("you").
func YouVoiceInsight(s string) string {
	s = strings.TrimSpace(s)
	for _, sub := range insightOwnerSubs {
		s = sub.pat.ReplaceAllString(s, sub.repl)
	}
	return strings.TrimSpace(s)
}

func CleanDraft(s string) string {
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
	if i := strings.IndexAny(line, ",.—–-!?:"); i >= 0 {
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

func LooksLikeGreeting(s string) bool {
	line := firstOpenLine(s)
	if line == "" || weakOpener(s) {
		return false
	}
	low := strings.ToLower(line)
	if model.ContainsAny(low, " here", "wrapping up", "workweek", "winding down the") {
		return false
	}
	if model.ContainsAny(low, "good morning", "good afternoon", "good evening", "good night",
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
			strings.HasPrefix(low, w+" -") || strings.HasPrefix(low, w+"-") ||
			strings.HasPrefix(low, w+" —") || strings.HasPrefix(low, w+"—") ||
			strings.HasPrefix(low, w+" –") || strings.HasPrefix(low, w+"–") {
			return true
		}
	}
	return false
}

func fallbackGreeting(localNow time.Time) string {
	h := localNow.Hour()
	switch {
	case h < 12:
		return "Good morning."
	case h < 17:
		return "Good afternoon."
	default:
		return "Good evening."
	}
}
