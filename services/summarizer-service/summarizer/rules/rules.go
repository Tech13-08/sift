package rules

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"sift/summarizer-service/summarizer/digestlog"
	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
)

var (
	cmdMute      = regexp.MustCompile(`(?i)^(?:please )?(?:mute|ignore|avoid|don't include|do not include|dont include|leave out|don't show(?: me)?|do not show(?: me)?|stop showing(?: me)?)\s+(.+?)$`)
	cmdUnmute    = regexp.MustCompile(`(?i)^(?:please )?unmute\s+(.+?)$`)
	cmdAlways    = regexp.MustCompile(`(?i)^(?:please )?always show\s+(.+?)$`)
	cmdJobs      = regexp.MustCompile(`(?i)^(?:please )?(?:set\s+)?(?:only show(?: me)? )?(?:new )?(?:job alerts?|jobs?)(?: search(?: ones)?)?(?:\s+for|\s+if they match|\s+if|:)?\s+(.+)$`)
	cmdList      = regexp.MustCompile(`(?i)^(?:list |show )?rules?$`)
	cmdRemoveAll = regexp.MustCompile(`(?i)^(?:please )?(?:clear|forget|remove|delete) (?:all )?(?:of )?(?:my )?rules$`)
	cmdRemoveN   = regexp.MustCompile(`(?i)^(?:please )?(?:remove|forget|delete|drop)(?: rule)?\s+#?(\d+)$`)
	cmdRemove    = regexp.MustCompile(`(?i)^(?:please )?(?:remove|forget|delete|drop|stop watching|don't watch|do not watch|unwatch)\s+(.+?)$`)
	cmdHelp      = regexp.MustCompile(`(?i)^(?:please )?(?:help|commands|what can you do)\??$`)
	cmdColorN    = regexp.MustCompile(`(?i)^(?:please )?(?:change|make|paint|color|set)(?: it| this)?(?: rule)?\s+#?(\d+)\s+(?:to |as )?([a-zA-Z]+)$`)
	cmdColor     = regexp.MustCompile(`(?i)^(?:please )?(?:change|make|paint|color|set)\s+(.+?)\s+(?:to |as )?([a-zA-Z]+)$`)
	cmdColorIs   = regexp.MustCompile(`(?i)^(.+?)\s+is\s+([a-zA-Z]+)$`)
	reRemoveNums = regexp.MustCompile(`(?i)(?:remove|forget|delete|drop)(?:\s+rules?)?\s+((?:#?\d+(?:\s*,\s*(?:and\s+)?|\s+and\s+|\s+))+#?\d+|#?\d+)`)
	reColorNum   = regexp.MustCompile(`(?i)(?:change|make|paint|color|set)(?:\s+it|\s+this)?(?:\s+[A-Za-z]+)?\s+#?(\d+)\s+(?:to\s+|as\s+)?([a-zA-Z]+)`)
)

func ParseMailCommand(text string) model.ParsedCommand {
	s := strings.TrimSpace(text)
	s = strings.Trim(s, `"'`)
	if s == "" {
		return model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}
	}
	if cmdHelp.MatchString(s) {
		return model.ParsedCommand{Action: "help"}
	}
	if cmdList.MatchString(s) {
		return model.ParsedCommand{Action: "list"}
	}
	if cmdRemoveAll.MatchString(s) {
		return model.ParsedCommand{Action: "remove_all"}
	}
	if m := cmdRemoveN.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}
		}
		return model.ParsedCommand{Action: "remove_n", Index: n}
	}
	if m := cmdUnmute.FindStringSubmatch(s); m != nil {
		raw := strings.TrimSpace(m[1])
		if target := NormalizeMuteTarget(raw); target != "" {
			return model.ParsedCommand{Action: "unmute", Pattern: target}
		}
		p := cleanRulePattern(raw)
		if p == "" {
			return model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}
		}
		return model.ParsedCommand{Action: "unmute", Pattern: p}
	}
	if m := cmdRemove.FindStringSubmatch(s); m != nil {
		p := cleanRemovedPattern(m[1])
		if p == "" {
			return model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}
		}
		return model.ParsedCommand{Action: "remove", Pattern: p}
	}
	if m := cmdColorN.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil && n >= 1 {
			if _, _, ok := ParseColorName(m[2]); ok {
				return model.ParsedCommand{Action: "color", Index: n, ColorName: m[2]}
			}
		}
	}
	if m := cmdColor.FindStringSubmatch(s); m != nil {
		if _, _, ok := ParseColorName(m[2]); ok {
			p := cleanRemovedPattern(m[1])
			return model.ParsedCommand{Action: "color", Pattern: p, ColorName: m[2]}
		}
	}
	if m := cmdColorIs.FindStringSubmatch(s); m != nil {
		if _, _, ok := ParseColorName(m[2]); ok {
			p := cleanRemovedPattern(m[1])
			if p != "" {
				return model.ParsedCommand{Action: "color", Pattern: p, ColorName: m[2]}
			}
		}
	}
	if m := cmdAlways.FindStringSubmatch(s); m != nil {
		p := cleanRulePattern(m[1])
		if p == "" {
			return model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}
		}
		return model.ParsedCommand{Action: model.RuleAlwaysShow, Pattern: p}
	}
	if m := cmdMute.FindStringSubmatch(s); m != nil {
		raw := strings.TrimSpace(m[1])
		if target := NormalizeMuteTarget(raw); target != "" {
			return model.ParsedCommand{Action: model.RuleMute, Pattern: target}
		}
		p := cleanRulePattern(raw)
		return model.ParsedCommand{Action: model.RuleMute, Pattern: p, Reply: MuteNeedEmailHelp()}
	}
	if m := cmdJobs.FindStringSubmatch(s); m != nil {
		p := strings.TrimSpace(m[1])
		if p == "" {
			return model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}
		}
		return model.ParsedCommand{Action: model.RuleJobFilter, Pattern: p}
	}
	return model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}
}

func ClassifyInbox(content string) (model.InboxRoute, model.ParsedCommand, []model.RuleEdit) {
	content = strings.TrimSpace(content)
	if content == "" {
		return model.InboxEmpty, model.ParsedCommand{Action: "unknown", Reply: RuleHelp()}, nil
	}
	if looksLikeAck(content) {
		return model.InboxAck, model.ParsedCommand{}, nil
	}
	if name, arg, ok := ParseSlashText(content); ok {
		switch name {
		case model.SlashQuery:
			return model.InboxInsight, model.ParsedCommand{Action: "slash_query", Reply: arg}, nil
		case model.SlashRule:
			return model.InboxInterpret, model.ParsedCommand{Action: "slash_rule", Reply: arg}, nil
		case model.SlashMute:
			return model.InboxCommand, model.ParsedCommand{Action: model.RuleMute, Pattern: arg}, nil
		case model.SlashRules:
			return model.InboxCommand, model.ParsedCommand{Action: "list"}, nil
		case model.SlashHelp:
			return model.InboxCommand, model.ParsedCommand{Action: "help"}, nil
		}
	}
	if edits := ParseRuleEdits(content); len(edits) > 0 {
		return model.InboxEdits, model.ParsedCommand{}, edits
	}
	cmd := ParseMailCommand(content)
	if cmd.Action != "unknown" {
		// "ignore/don't include <topic>" without an email is a category skip, not a sender mute.
		if cmd.Action == model.RuleMute && NormalizeMuteTarget(cmd.Pattern) == "" {
			return model.InboxInterpret, cmd, nil
		}
		return model.InboxCommand, cmd, nil
	}
	if LooksLikeInsight(content) {
		return model.InboxInsight, cmd, nil
	}
	return model.InboxInterpret, cmd, nil
}

func looksLikeAck(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".!?")
	switch s {
	case "thanks", "thank you", "thx", "ty", "ok", "okay", "cool", "got it",
		"nice", "k", "kk", "np", "sure", "yep", "yes", "yeah":
		return true
	default:
		return false
	}
}

func ParseRuleEdits(text string) []model.RuleEdit {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil
	}
	var edits []model.RuleEdit
	if m := reRemoveNums.FindStringSubmatch(s); m != nil {
		idxs := ParseIndexList(m[1])
		if len(idxs) > 0 {
			edits = append(edits, model.RuleEdit{Action: "remove_n", Indexes: idxs})
		}
	}
	for _, m := range reColorNum.FindAllStringSubmatch(s, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			continue
		}
		if _, _, ok := ParseColorName(m[2]); !ok {
			continue
		}
		edits = append(edits, model.RuleEdit{Action: "color", Index: n, ColorName: m[2]})
	}
	return edits
}

func ParseIndexList(s string) []int {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "#", "")
	s = strings.ReplaceAll(s, ",", " ")
	s = strings.ReplaceAll(s, " and ", " ")
	var out []int
	seen := map[int]bool{}
	for _, p := range strings.Fields(s) {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

func cleanRulePattern(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".")
	s = strings.TrimSpace(s)
	for _, suf := range []string{
		" emails again", " email again", " mails again", " mail again",
		" emails", " email", " mails", " mail", " again",
	} {
		if strings.HasSuffix(strings.ToLower(s), suf) {
			s = strings.TrimSpace(s[:len(s)-len(suf)])
		}
	}
	s = strings.Trim(s, `"'`)
	// Drop leading/trailing mail noise tokens without lowercasing the brand.
	fields := strings.Fields(s)
	var kept []string
	for _, w := range fields {
		if muteNoiseWord(w) {
			continue
		}
		kept = append(kept, w)
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func muteNoiseWord(w string) bool {
	switch strings.ToLower(strings.Trim(w, `.,;:"'`)) {
	case "the", "a", "an", "and", "or", "of", "to", "for", "from", "with",
		"any", "all", "my", "me", "mail", "mails", "email", "emails",
		"message", "messages", "inbox", "matching", "kind", "this", "that",
		"those", "these", "as", "please", "again":
		return true
	default:
		return false
	}
}

// Collapse "icici mails" / "ICICI mail" / dupes → "icici".
func NormalizeMutePattern(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "-", " ")
	seen := map[string]bool{}
	var words []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, `.,;:"'`)
		if w == "" || muteNoiseWord(w) || len(w) < 2 || seen[w] {
			continue
		}
		seen[w] = true
		words = append(words, w)
	}
	return strings.Join(words, " ")
}

func muteTokenKey(s string) string {
	parts := strings.Fields(NormalizeMutePattern(s))
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func cleanRemovedPattern(s string) string {
	s = cleanRulePattern(s)
	low := strings.ToLower(s)
	for _, pre := range []string{"the ", "my ", "that ", "this "} {
		if strings.HasPrefix(low, pre) {
			s = strings.TrimSpace(s[len(pre):])
			low = strings.ToLower(s)
		}
	}
	for _, suf := range []string{" rule", " watch", " instruction", " mute"} {
		if strings.HasSuffix(low, suf) {
			s = strings.TrimSpace(s[:len(s)-len(suf)])
			low = strings.ToLower(s)
		}
	}
	return strings.TrimSpace(s)
}

func LooksLikeHelpRequest(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	if cmdHelp.MatchString(low) {
		return true
	}
	return model.ContainsAny(low, "what can you do", "how do i use", "what commands", "how does this work")
}

func LooksLikeJobPreference(s string) bool {
	low := strings.ToLower(s)
	return model.ContainsAny(low, "job alert", "job alerts", "new grad", "early career", "full time role", "full-time")
}

func JobPreferenceCriteria(s string) string {
	c := ParseMailCommand(s)
	if c.Action == model.RuleJobFilter && strings.TrimSpace(c.Pattern) != "" {
		return strings.TrimSpace(c.Pattern)
	}
	return ""
}

func RuleHelp() string {
	return "Try `/help` for how to hide senders, skip kinds of mail, or ask about your inbox."
}

func RuleHelpFull() string {
	return strings.TrimSpace(`
Sift watches your linked inboxes and sends a short daily digest.

**Hide one company or person**
Use their email or domain — not a brand word.
• ` + "`/mute @extern.com`" + ` — hide everyone from that domain
• ` + "`/mute community@extern.com`" + ` — hide that exact address
• ` + "`unmute @extern.com`" + ` — undo

**Skip a kind of mail** (ads, newsletters, sign-in noise, …)
Describe the topic with ` + "`/rule`" + `. Wording like skip / ignore / don't include all work the same.
• ` + "`/rule skip job-site product ads`" + `
• ` + "`/rule ignore new sign-in emails`" + `
Don't use ` + "`/mute`" + ` for topics — mute is only for addresses.

**Keep what matters**
Be specific — name the sender, product, or exact kind of mail. Vague keeps ("important stuff") are easy for the model to over-apply. Digest color uses the model's best matching keep rule only when it is confident the email body fits that rule.
• Good: ` + "`/rule keep Empower account and payment alerts`" + `
• Good: ` + "`/rule set job alerts for early-career US roles`" + `
• Weak: ` + "`/rule treat finance updates as important`" + `
New keep rules start grey — say ` + "`make it purple`" + ` (or blue, green, …) to color them.

**Ask about mail**
• ` + "`/query did I get any Hyundai emails today?`" + `

**See or change rules**
• ` + "`/rules`" + ` — list your rules
• ` + "`remove 2`" + ` — delete rule 2 from that list
• ` + "`clear rules`" + ` — remove all of them
`)
}

func ApplyRules(ctx context.Context, messages []model.IngestedMessage, facts []model.MessageFacts, rules []model.MailRule) (kept []model.MessageFacts, noise int) {
	jobTokens := jobFilterTokens(rules)
	for i, msg := range messages {
		f := model.MessageFacts{Kind: model.KindNotice}
		if i < len(facts) {
			f = facts[i]
		}
		if AlwaysShows(rules, msg) {
			f.Kind = model.KindNotice
			if strings.TrimSpace(f.Title) == "" {
				f.Title = model.CollapseSpace(msg.Subject)
			}
			if strings.TrimSpace(f.Summary) == "" {
				f.Summary = mail.FallbackLine(msg)
			}
			f.Mailbox = msg.Mailbox
			f.Color = ColorForFact(rules, msg, f)
			f.Outcome = mail.DecisionOutcome("keep", "always-show", "always-show rule")
			digestlog.LogDecide(ctx, "keep", "always-show", msg.From, msg.Subject, "always-show rule", msg.Body)
			kept = append(kept, f)
			continue
		}
		if msg.ReplyToMe {
			f = mail.KeepReplyToMe(msg, f)
			f.Mailbox = msg.Mailbox
			f.Color = ColorForFact(rules, msg, f)
			kept = append(kept, f)
			continue
		}
		if Muted(ctx, rules, msg) {
			noise++
			continue
		}
		if ClaimedByWatch(rules, msg, f) {
			f = mail.KeepWatchedMail(msg, f)
			f.Mailbox = msg.Mailbox
			f.Color = ColorForFact(rules, msg, f)
			f.Outcome = mail.DecisionOutcome("keep", "watch", "matches a watch/keep rule")
			kept = append(kept, f)
			continue
		}
		if f.Kind == model.KindPromo || f.Kind == "" {
			noise++
			digestlog.LogDecide(ctx, "skip", "rules", msg.From, msg.Subject, model.FirstNonEmpty(f.Outcome, "marked promo"), msg.Body)
			continue
		}
		if len(jobTokens) > 0 && mail.LooksJobish(f.What+" "+f.Title+" "+msg.Subject+" "+msg.From) && !matchesJobFilter(f, msg, jobTokens) {
			noise++
			digestlog.LogDecide(ctx, "skip", "job-filter", msg.From, msg.Subject, "job-ish but does not match watch criteria", msg.Body)
			continue
		}
		if strings.TrimSpace(f.Title) == "" && strings.TrimSpace(f.Summary) == "" && mail.CompileLine(f) == "" {
			noise++
			digestlog.LogDecide(ctx, "skip", "rules", msg.From, msg.Subject, "empty title/summary", msg.Body)
			continue
		}
		if strings.TrimSpace(f.Title) == "" {
			f.Title = model.CollapseSpace(msg.Subject)
		}
		if strings.TrimSpace(f.Summary) == "" {
			f.Summary = mail.CompileLine(f)
		}
		f.Mailbox = msg.Mailbox
		f.Color = ColorForFact(rules, msg, f)
		digestlog.LogDecide(ctx, "keep", "rules", msg.From, msg.Subject, model.FirstNonEmpty(f.Outcome, "passed filters"), msg.Body)
		kept = append(kept, f)
	}
	return kept, noise
}

func Muted(ctx context.Context, rules []model.MailRule, msg model.IngestedMessage) bool {
	_, ok := FindMute(ctx, rules, msg, model.MessageFacts{})
	return ok
}

// mute/skip/ignore/avoid/"don't include" all hard-hide on match.
func FindMute(ctx context.Context, rules []model.MailRule, msg model.IngestedMessage, f model.MessageFacts) (string, bool) {
	for _, r := range rules {
		if r.Type != model.RuleMute {
			continue
		}
		if MuteMatchesSender(r.Pattern, msg.From) {
			return "Muted " + NormalizeMuteEmail(r.Pattern), true
		}
	}
	return SkippedByInstruction(ctx, rules, msg, f)
}

// Hide-style instructions (NLP) act like mutes.
func SkippedByInstruction(ctx context.Context, rules []model.MailRule, msg model.IngestedMessage, f model.MessageFacts) (string, bool) {
	hay := strings.ToLower(msg.From + " " + msg.Subject + " " + msg.Body + " " + f.Title + " " + f.Summary + " " + f.What)
	hay = strings.ReplaceAll(hay, "-", " ")
	for _, r := range rules {
		if r.Type == model.RuleMute || r.Type == model.RuleAlwaysShow || r.Type == model.RuleJobFilter {
			continue
		}
		if r.Type != model.RuleInstruction && r.Type != "" {
			continue
		}
		if !InstructionIsHide(ctx, r) {
			continue
		}
		needles := SkipInstructionNeedles(r)
		// Do not merge ClassifyHideIntent "mutes" — the model invents broad needles
		// (e.g. "sign in" from "ignore new sign in emails") that false-hide unrelated mail.
		for _, needle := range needles {
			if needle != "" && strings.Contains(hay, strings.ToLower(needle)) {
				return "Muted (" + ruleLabel(r) + ")", true
			}
		}
	}
	return "", false
}

func SkipInstructionNeedles(r model.MailRule) []string {
	raw := strings.ToLower(strings.TrimSpace(r.Instruction + " " + r.Pattern))
	raw = strings.ReplaceAll(raw, "-", " ")
	for _, cut := range []string{
		"treat as not important:", "treat as not important", "not important:",
		"do not mention mail matching", "don't mention mail matching",
		"do not mention", "don't mention", "do not show", "don't show",
		"don't include", "do not include", "dont include",
		"never show", "never mention", "stop showing", "stop mentioning",
		"ignore emails", "ignore email", "ignore mails", "ignore mail", "ignore ",
		"skip emails", "skip email", "skip mails", "skip mail", "skip ",
		"avoid emails", "avoid email", "avoid mails", "avoid mail", "avoid ",
		"leave out", "leave off", "exclude ", "filter out",
		"not interested in", "not interested",
		"i don't need", "i do not need", "don't need", "do not need",
	} {
		raw = strings.ReplaceAll(raw, cut, " ")
	}
	phrase := NormalizeMutePattern(raw)
	if phrase == "" || !UsableMutePattern(phrase) {
		return nil
	}
	// One phrase only - never explode into bigram spam.
	return []string{phrase}
}

func AlwaysShows(rules []model.MailRule, msg model.IngestedMessage) bool {
	hay := ruleHaystack(msg)
	for _, r := range rules {
		if r.Type == model.RuleAlwaysShow && strings.Contains(hay, strings.ToLower(r.Pattern)) {
			return true
		}
	}
	return false
}

func ClaimedByWatch(rules []model.MailRule, msg model.IngestedMessage, f model.MessageFacts) bool {
	if AlwaysShows(rules, msg) {
		return true
	}
	tokens := jobFilterTokens(rules)
	hay := strings.ToLower(msg.From + " " + msg.Subject + " " + f.Who + " " + f.What + " " + f.Title)
	return len(tokens) > 0 && mail.LooksJobish(hay) && matchesJobFilter(f, msg, tokens)
}

func ruleHaystack(msg model.IngestedMessage) string {
	return strings.ToLower(msg.From + " " + msg.Subject)
}

func jobFilterTokens(rules []model.MailRule) []string {
	var raw string
	for _, r := range rules {
		if r.Type == model.RuleJobFilter {
			raw = r.Pattern
		}
	}
	if raw == "" {
		return nil
	}
	return jobWatchNeedles(raw)
}

func jobWatchNeedles(raw string) []string {
	raw = strings.ToLower(strings.ReplaceAll(raw, "-", " "))
	stop := map[string]bool{
		"only": true, "show": true, "me": true, "job": true, "jobs": true,
		"search": true, "ones": true, "if": true, "they": true, "match": true, "the": true,
		"a": true, "an": true, "or": true, "and": true, "for": true, "with": true,
		"criteria": true, "such": true, "that": true, "within": true, "towards": true,
		"toward": true, "targeted": true, "roles": true, "role": true, "across": true,
	}
	var needles []string
	for _, clause := range strings.Split(raw, ",") {
		var words []string
		for _, part := range strings.FieldsFunc(clause, func(r rune) bool {
			return unicode.IsSpace(r) || r == '/'
		}) {
			part = strings.Trim(part, `."'`)
			if part == "" || stop[part] {
				continue
			}
			words = append(words, part)
		}
		if len(words) == 0 {
			continue
		}
		if len(words) == 1 {
			needles = append(needles, jobTokenNeedles(words[0])...)
			continue
		}
		for i := 0; i+1 < len(words); i++ {
			phrase := words[i] + " " + words[i+1]
			needles = append(needles, phrase)
			if strings.HasSuffix(phrase, "s") {
				needles = append(needles, strings.TrimSuffix(phrase, "s"))
			}
		}
	}
	return needles
}

func matchesJobFilter(f model.MessageFacts, msg model.IngestedMessage, tokens []string) bool {
	hay := strings.ToLower(f.What + " " + f.Who + " " + msg.Subject + " " + msg.From)
	hay = strings.ReplaceAll(hay, "-", " ")
	for _, t := range tokens {
		if t != "" && strings.Contains(hay, t) {
			return true
		}
	}
	return false
}

func jobTokenNeedles(t string) []string {
	switch t {
	case "ml":
		return []string{"ml", "machine learning"}
	case "swe":
		return []string{"swe", "software engineer"}
	default:
		return []string{t}
	}
}

func ruleAppendixLine(r model.MailRule) string {
	if strings.TrimSpace(r.Instruction) != "" {
		return strings.TrimSpace(r.Instruction)
	}
	switch r.Type {
	case model.RuleMute:
		return "Do not mention mail matching " + r.Pattern + "."
	case model.RuleAlwaysShow:
		return "Always mention mail matching " + r.Pattern + "."
	case model.RuleJobFilter:
		return "Only mention job-search mail if it matches: " + r.Pattern + "."
	default:
		return strings.TrimSpace(r.Pattern)
	}
}

func RulePromptAppendix(rules []model.MailRule) string {
	var keep, skip []string
	keepN := 0
	for _, r := range rules {
		line := ruleAppendixLine(r)
		if line == "" {
			continue
		}
		if RuleShowsColor(r) {
			keepN++
			keep = append(keep, fmt.Sprintf("[%d] %s", keepN, line))
			continue
		}
		skip = append(skip, "- "+line)
	}
	if len(keep) == 0 && len(skip) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Watch/keep rules win when they fit the email. Skip rules only hide the skipped subclass, not the watched item.\n")
	if len(keep) > 0 {
		b.WriteString("Keep rules (set matched_rule to that number only when keep=true AND the body clearly fits that rule; set rule_confidence 0-100 for that pick, or 0 if matched_rule=0):\n")
		b.WriteString(strings.Join(keep, "\n"))
		if len(skip) > 0 {
			b.WriteByte('\n')
		}
	}
	if len(skip) > 0 {
		b.WriteString("Skip / hide (matched_rule=0; use keep=false):\n")
		b.WriteString(strings.Join(skip, "\n"))
	}
	return strings.TrimSpace(b.String())
}

func LoadRules(ctx context.Context, db *sql.DB, userID string) ([]model.MailRule, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, rule_type, pattern, COALESCE(instruction, ''), COALESCE(color, 0)
		FROM user_mail_rules WHERE user_id = $1 ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []model.MailRule
	for rows.Next() {
		var r model.MailRule
		if err := rows.Scan(&r.ID, &r.Type, &r.Pattern, &r.Instruction, &r.Color); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func SanitizeUserRuleParse(p model.UserRuleParse) model.UserRuleParse {
	var mutes []string
	seenMute := map[string]bool{}
	addMute := func(m string) {
		email := NormalizeMuteTarget(m)
		if email == "" || seenMute[email] {
			return
		}
		seenMute[email] = true
		mutes = append(mutes, email)
	}
	for _, m := range p.Mutes {
		addMute(m)
	}
	var ins []string
	seenIns := map[string]bool{}
	for _, s := range p.Instructions {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if !UsableInstruction(s) || seenIns[key] {
			continue
		}
		// Hide text with an embedded sender email/domain → mute that target.
		if IsPureHideInstruction(s) {
			if t := NormalizeMuteTarget(s); t != "" {
				addMute(t)
				continue
			}
			// Category skip (ads, tech news, sign-in) stays an instruction.
			seenIns[key] = true
			ins = append(ins, s)
			continue
		}
		seenIns[key] = true
		ins = append(ins, s)
	}
	p.Mutes = mutes
	p.Instructions = ins
	return p
}

func UsableMutePattern(s string) bool {
	s = NormalizeMutePattern(s)
	if len(s) < 3 {
		return false
	}
	if model.ContainsAny(s,
		"job hunting", "hunting site", "job site", "this kind", "that kind",
		"kind of", "advertising", "product ads", "newsletter") {
		return false
	}
	switch s {
	case "job", "jobs", "email", "emails", "mail", "mails", "site", "sites",
		"product", "alert", "alerts", "hunting", "new", "sign", "in":
		return false
	}
	// Reject phrases that are only noise leftovers or single filler words.
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return false
	}
	return true
}

func UsableInstruction(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	low := strings.ToLower(s)
	if model.ContainsAny(low, "this kind", "that kind", "this type", "that type", "this sort") {
		return false
	}
	return true
}

// Hide-only rule, no keep exception. Prefer ClassifyHideIntent; this is offline fallback.
func IsPureHideInstruction(s string) bool {
	if LooksLikeKeepPreference(s) {
		return false
	}
	low := strings.ToLower(s)
	if model.ContainsAny(low, "keep ", "except ", "but keep", "still show", "still mention") {
		return false
	}
	if v, ok := HideIntentCache.Load(strings.ToLower(strings.TrimSpace(s))); ok {
		return v.(model.HideIntent).Hide
	}
	return LooksLikeSkipPreferenceHeuristic(s)
}

func alreadyHasMute(rules []model.MailRule, pattern string) bool {
	return alreadyHasPattern(rules, model.RuleMute, pattern)
}

// True if general matches the same thing as specific, or a broader set.
func MuteGeneralizes(general, specific string) bool {
	g := NormalizeMuteTarget(general)
	s := NormalizeMuteTarget(specific)
	if g != "" && s != "" {
		if g == s {
			return true
		}
		if strings.HasPrefix(g, "@") {
			if strings.HasPrefix(s, "@") {
				return muteDomainCovers(g[1:], "x@"+s[1:])
			}
			return muteDomainCovers(g[1:], s)
		}
		return false
	}
	gPat := NormalizeMutePattern(general)
	sPat := NormalizeMutePattern(specific)
	if gPat == "" || sPat == "" {
		return false
	}
	if gPat == sPat || muteTokenKey(gPat) == muteTokenKey(sPat) {
		return true
	}
	return len(gPat) >= 3 && strings.Contains(sPat, gPat)
}

func MuteAlreadyCovered(rules []model.MailRule, pattern string) bool {
	pattern = NormalizeMutePattern(pattern)
	for _, r := range rules {
		if r.Type == model.RuleMute && MuteGeneralizes(r.Pattern, pattern) {
			return true
		}
	}
	return false
}

func CollapseRelatedMutes(mutes []string) []string {
	var cleaned []string
	seenKey := map[string]bool{}
	for _, m := range mutes {
		m = NormalizeMutePattern(m)
		if m == "" || !UsableMutePattern(m) {
			continue
		}
		key := muteTokenKey(m)
		if seenKey[key] {
			continue
		}
		seenKey[key] = true
		cleaned = append(cleaned, m)
	}
	keep := make([]bool, len(cleaned))
	for i := range cleaned {
		keep[i] = true
	}
	for i := range cleaned {
		for j := range cleaned {
			if i == j || !keep[i] || !keep[j] {
				continue
			}
			// Prefer the broader (shorter covering) mute.
			if MuteGeneralizes(cleaned[i], cleaned[j]) && !MuteGeneralizes(cleaned[j], cleaned[i]) {
				keep[j] = false
			} else if MuteGeneralizes(cleaned[j], cleaned[i]) && !MuteGeneralizes(cleaned[i], cleaned[j]) {
				keep[i] = false
			} else if muteTokenKey(cleaned[i]) == muteTokenKey(cleaned[j]) {
				// Same tokens; keep the shorter spelling.
				if len(cleaned[i]) <= len(cleaned[j]) {
					keep[j] = false
				} else {
					keep[i] = false
				}
			}
		}
	}
	var out []string
	for i, m := range cleaned {
		if keep[i] {
			out = append(out, m)
		}
	}
	return out
}

// Clean mute list for save - sender emails only.
func MutesForSave(userText string, mutes []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range mutes {
		email := NormalizeMuteEmail(m)
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		out = append(out, email)
	}
	if email, ok := ExtractMuteEmail(userText); ok && !seen[email] {
		out = append(out, email)
	}
	return out
}

func InstructionRedundant(ctx context.Context, rules []model.MailRule, instruction string) bool {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return false
	}
	low := strings.ToLower(instruction)
	for _, r := range rules {
		if r.Type == model.RuleMute {
			p := strings.ToLower(strings.TrimSpace(r.Pattern))
			if p != "" && strings.Contains(low, p) {
				return true
			}
		}
		if r.Type == model.RuleInstruction {
			other := strings.TrimSpace(r.Instruction)
			if strings.EqualFold(other, instruction) {
				return true
			}
			if hideInstructionsOverlap(ctx, instruction, other) {
				return true
			}
		}
	}
	return false
}

func hideInstructionsOverlap(ctx context.Context, a, b string) bool {
	if !isSkipInstruction(model.MailRule{Type: model.RuleInstruction, Instruction: a}) ||
		!isSkipInstruction(model.MailRule{Type: model.RuleInstruction, Instruction: b}) {
		return false
	}
	na := SkipInstructionNeedles(model.MailRule{Instruction: a, Pattern: a})
	nb := SkipInstructionNeedles(model.MailRule{Instruction: b, Pattern: b})
	if hi, err := ClassifyHideIntent(ctx, a); err == nil {
		na = append(na, hi.Mutes...)
	}
	if hi, err := ClassifyHideIntent(ctx, b); err == nil {
		nb = append(nb, hi.Mutes...)
	}
	for _, x := range na {
		for _, y := range nb {
			if MuteGeneralizes(x, y) || MuteGeneralizes(y, x) {
				return true
			}
		}
	}
	return false
}

// Insert mute; drop narrower mutes and covered hide instructions.
func upsertMuteMerging(ctx context.Context, db *sql.DB, userID, pattern string, existing []model.MailRule) ([]model.MailRule, error) {
	pattern = NormalizeMuteEmail(pattern)
	if pattern == "" {
		return existing, nil
	}
	var dropIDs []string
	var next []model.MailRule
	for _, r := range existing {
		switch {
		case r.Type == model.RuleMute && MuteGeneralizes(pattern, r.Pattern) && muteTokenKey(r.Pattern) != muteTokenKey(pattern):
			// New mute is broader or cleans an equivalent mess - drop the old one.
			if r.ID != "" {
				dropIDs = append(dropIDs, r.ID)
			}
			continue
		case r.Type == model.RuleMute && MuteGeneralizes(r.Pattern, pattern):
			// Already covered by a broader or equal mute.
			return existing, nil
		case r.Type == model.RuleInstruction && instructionCoveredByMute(r, pattern):
			if r.ID != "" {
				dropIDs = append(dropIDs, r.ID)
			}
			continue
		}
		next = append(next, r)
	}
	if len(dropIDs) > 0 {
		if err := deleteRuleIDs(ctx, db, userID, dropIDs); err != nil {
			return existing, err
		}
	}
	if err := insertRule(ctx, db, userID, model.RuleMute, pattern, ""); err != nil {
		return existing, err
	}
	return append(next, model.MailRule{Type: model.RuleMute, Pattern: pattern}), nil
}

func instructionCoveredByMute(r model.MailRule, mute string) bool {
	text := strings.ToLower(strings.TrimSpace(r.Instruction + " " + r.Pattern))
	m := strings.ToLower(strings.TrimSpace(mute))
	if m == "" || text == "" {
		return false
	}
	if !isSkipInstruction(r) && !LooksLikeSkipPreferenceHeuristic(text) {
		return false
	}
	return strings.Contains(text, m)
}

// Normalize mute spellings; drop overlaps and covered hide instructions.
func pruneRedundantRules(ctx context.Context, db *sql.DB, userID string) (int, error) {
	rules, err := LoadRules(ctx, db, userID)
	if err != nil {
		return 0, err
	}
	nChanged := 0
	var dropIDs []string
	dropped := map[string]bool{}
	mark := func(id string) {
		if id != "" && !dropped[id] {
			dropped[id] = true
			dropIDs = append(dropIDs, id)
		}
	}
	// Rewrite garbled mute patterns to their normalized form (icici mails → icici).
	for i, r := range rules {
		if r.Type != model.RuleMute || r.ID == "" {
			continue
		}
		n := NormalizeMutePattern(r.Pattern)
		if n == "" || !UsableMutePattern(n) {
			mark(r.ID)
			continue
		}
		if n != r.Pattern {
			if _, err := db.ExecContext(ctx, `
				UPDATE user_mail_rules SET pattern = $1, instruction = NULL
				WHERE id = $2::uuid AND user_id = $3::uuid
			`, n, r.ID, userID); err != nil {
				return 0, err
			}
			rules[i].Pattern = n
			rules[i].Instruction = ""
			nChanged++
		}
	}
	// Narrower mute covered by a broader mute, or same tokens after normalize.
	for i, a := range rules {
		if a.Type != model.RuleMute || dropped[a.ID] {
			continue
		}
		for j, b := range rules {
			if i == j || b.Type != model.RuleMute || dropped[b.ID] {
				continue
			}
			if muteTokenKey(a.Pattern) == muteTokenKey(b.Pattern) {
				an, bn := NormalizeMutePattern(a.Pattern), NormalizeMutePattern(b.Pattern)
				if a.Pattern != an || (b.Pattern == bn && len(a.Pattern) > len(b.Pattern)) {
					mark(a.ID)
				} else {
					mark(b.ID)
				}
				continue
			}
			if MuteGeneralizes(a.Pattern, b.Pattern) && !MuteGeneralizes(b.Pattern, a.Pattern) {
				mark(b.ID)
			}
		}
	}
	// Hide instructions covered by a mute, or overlapping hide instructions (keep first).
	for i, a := range rules {
		if dropped[a.ID] {
			continue
		}
		if a.Type == model.RuleInstruction {
			for _, m := range rules {
				if m.Type == model.RuleMute && !dropped[m.ID] && instructionCoveredByMute(a, m.Pattern) {
					mark(a.ID)
					break
				}
			}
		}
		if dropped[a.ID] || a.Type != model.RuleInstruction {
			continue
		}
		for j := i + 1; j < len(rules); j++ {
			b := rules[j]
			if b.Type != model.RuleInstruction || dropped[b.ID] {
				continue
			}
			if hideInstructionsOverlap(ctx, a.Instruction, b.Instruction) {
				mark(b.ID)
			}
		}
	}
	if len(dropIDs) > 0 {
		if err := deleteRuleIDs(ctx, db, userID, dropIDs); err != nil {
			return 0, err
		}
	}
	return nChanged + len(dropIDs), nil
}

func alreadyHasPattern(rules []model.MailRule, typ, pattern string) bool {
	want := strings.ToLower(strings.TrimSpace(pattern))
	for _, r := range rules {
		if r.Type == typ && strings.ToLower(strings.TrimSpace(r.Pattern)) == want {
			return true
		}
	}
	return false
}

func alreadyHasInstruction(rules []model.MailRule, text string) bool {
	want := strings.ToLower(strings.TrimSpace(text))
	for _, r := range rules {
		if r.Type == model.RuleInstruction && strings.ToLower(strings.TrimSpace(r.Instruction)) == want {
			return true
		}
	}
	return false
}

func insertRule(ctx context.Context, db *sql.DB, userID, ruleType, pattern, instruction string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO user_mail_rules (user_id, rule_type, pattern, instruction)
		VALUES ($1, $2, $3, $4)
	`, userID, ruleType, pattern, model.NullIfEmpty(instruction))
	return err
}

func deleteRules(ctx context.Context, db *sql.DB, userID, ruleType, pattern string) (int64, error) {
	res, err := db.ExecContext(ctx, `
		DELETE FROM user_mail_rules
		WHERE user_id = $1 AND rule_type = $2 AND lower(pattern) = lower($3)
	`, userID, ruleType, pattern)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func replaceJobFilter(ctx context.Context, db *sql.DB, userID, pattern string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM user_mail_rules WHERE user_id = $1 AND rule_type = $2`, userID, model.RuleJobFilter); err != nil {
		return err
	}
	return insertRule(ctx, db, userID, model.RuleJobFilter, pattern, "")
}

func FormatRules(rules []model.MailRule) string {
	if len(rules) == 0 {
		return "No rules yet."
	}
	var b strings.Builder
	b.WriteString("Your rules:")
	for i, r := range rules {
		fmt.Fprintf(&b, "\n%d. %s", i+1, ruleLabel(r))
		if RuleShowsColor(r) {
			fmt.Fprintf(&b, " · %s", colorLabel(r.Color))
		}
	}
	return b.String()
}

func RuleShowsColor(r model.MailRule) bool {
	switch r.Type {
	case model.RuleMute:
		return false
	case model.RuleAlwaysShow, model.RuleJobFilter:
		return true
	}
	return !isSkipInstruction(r)
}

func isSkipInstruction(r model.MailRule) bool {
	text := strings.TrimSpace(r.Instruction + " " + r.Pattern)
	if text == "" {
		return false
	}
	// Prefer cached NLP classification; heuristics only if the model is unreachable.
	if v, ok := HideIntentCache.Load(strings.ToLower(text)); ok {
		return v.(model.HideIntent).Hide
	}
	return IsPureHideInstruction(text)
}

func ruleLabel(r model.MailRule) string {
	if strings.TrimSpace(r.Instruction) != "" {
		return strings.TrimSpace(r.Instruction)
	}
	switch r.Type {
	case model.RuleMute:
		label := "mute " + r.Pattern
		if NormalizeMuteEmail(r.Pattern) == "" {
			label += " (inactive — use a sender email)"
		}
		return label
	case model.RuleAlwaysShow:
		return "always show " + r.Pattern
	case model.RuleJobFilter:
		return "only jobs matching " + r.Pattern
	default:
		return r.Type + ": " + r.Pattern
	}
}

func MatchingRules(rules []model.MailRule, needle string) []model.MailRule {
	needle = strings.ToLower(model.CollapseSpace(needle))
	if len(needle) < 2 {
		return nil
	}
	var out []model.MailRule
	for _, r := range rules {
		hay := strings.ToLower(strings.TrimSpace(r.Pattern + " " + r.Instruction + " " + ruleLabel(r)))
		if strings.Contains(hay, needle) {
			out = append(out, r)
		}
	}
	return out
}

func deleteRuleIDs(ctx context.Context, db *sql.DB, userID string, ids []string) error {
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			DELETE FROM user_mail_rules WHERE user_id = $1 AND id = $2::uuid
		`, userID, id); err != nil {
			return err
		}
	}
	return nil
}

func deleteAllRules(ctx context.Context, db *sql.DB, userID string) (int64, error) {
	res, err := db.ExecContext(ctx, `DELETE FROM user_mail_rules WHERE user_id = $1`, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func RemovedReply(gone []model.MailRule) string {
	if len(gone) == 0 {
		return "No rule matched."
	}
	var b strings.Builder
	if len(gone) == 1 {
		b.WriteString("Removed: ")
		b.WriteString(ruleLabel(gone[0]))
		return b.String() + "."
	}
	b.WriteString("Removed:")
	for _, r := range gone {
		b.WriteString("\n• ")
		b.WriteString(ruleLabel(r))
	}
	return b.String()
}
