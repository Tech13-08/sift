package main

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	ruleMute        = "mute"
	ruleAlwaysShow  = "always_show"
	ruleJobFilter   = "job_filter"
	ruleInstruction = "instruction"
)

type mailRule struct {
	ID          string
	Type        string
	Pattern     string
	Instruction string
	Color       int
}

type parsedCommand struct {
	Action    string
	Pattern   string
	Index     int
	ColorName string
	Reply     string
}

type inboxRoute string

const (
	inboxEmpty     inboxRoute = "empty"
	inboxAck       inboxRoute = "ack"
	inboxEdits     inboxRoute = "edits"
	inboxCommand   inboxRoute = "command"
	inboxInsight   inboxRoute = "insight"
	inboxInterpret inboxRoute = "interpret"
)

var (
	cmdMute      = regexp.MustCompile(`(?i)^(?:please )?(?:mute|don't show(?: me)?|do not show(?: me)?|stop showing(?: me)?)\s+(.+?)$`)
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

type ruleEdit struct {
	Action    string
	Index     int
	Indexes   []int
	ColorName string
}

func parseMailCommand(text string) parsedCommand {
	s := strings.TrimSpace(text)
	s = strings.Trim(s, `"'`)
	if s == "" {
		return parsedCommand{Action: "unknown", Reply: ruleHelp()}
	}
	if cmdHelp.MatchString(s) {
		return parsedCommand{Action: "help"}
	}
	if cmdList.MatchString(s) {
		return parsedCommand{Action: "list"}
	}
	if cmdRemoveAll.MatchString(s) {
		return parsedCommand{Action: "remove_all"}
	}
	if m := cmdRemoveN.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return parsedCommand{Action: "unknown", Reply: ruleHelp()}
		}
		return parsedCommand{Action: "remove_n", Index: n}
	}
	if m := cmdUnmute.FindStringSubmatch(s); m != nil {
		p := cleanRulePattern(m[1])
		if p == "" {
			return parsedCommand{Action: "unknown", Reply: ruleHelp()}
		}
		return parsedCommand{Action: "unmute", Pattern: p}
	}
	if m := cmdRemove.FindStringSubmatch(s); m != nil {
		p := cleanRemovedPattern(m[1])
		if p == "" {
			return parsedCommand{Action: "unknown", Reply: ruleHelp()}
		}
		return parsedCommand{Action: "remove", Pattern: p}
	}
	if m := cmdColorN.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil && n >= 1 {
			if _, _, ok := parseColorName(m[2]); ok {
				return parsedCommand{Action: "color", Index: n, ColorName: m[2]}
			}
		}
	}
	if m := cmdColor.FindStringSubmatch(s); m != nil {
		if _, _, ok := parseColorName(m[2]); ok {
			p := cleanRemovedPattern(m[1])
			return parsedCommand{Action: "color", Pattern: p, ColorName: m[2]}
		}
	}
	if m := cmdColorIs.FindStringSubmatch(s); m != nil {
		if _, _, ok := parseColorName(m[2]); ok {
			p := cleanRemovedPattern(m[1])
			if p != "" {
				return parsedCommand{Action: "color", Pattern: p, ColorName: m[2]}
			}
		}
	}
	if m := cmdAlways.FindStringSubmatch(s); m != nil {
		p := cleanRulePattern(m[1])
		if p == "" {
			return parsedCommand{Action: "unknown", Reply: ruleHelp()}
		}
		return parsedCommand{Action: ruleAlwaysShow, Pattern: p}
	}
	if m := cmdMute.FindStringSubmatch(s); m != nil {
		p := cleanRulePattern(m[1])
		if p == "" {
			return parsedCommand{Action: "unknown", Reply: ruleHelp()}
		}
		return parsedCommand{Action: ruleMute, Pattern: p}
	}
	if m := cmdJobs.FindStringSubmatch(s); m != nil {
		p := strings.TrimSpace(m[1])
		if p == "" {
			return parsedCommand{Action: "unknown", Reply: ruleHelp()}
		}
		return parsedCommand{Action: ruleJobFilter, Pattern: p}
	}
	return parsedCommand{Action: "unknown", Reply: ruleHelp()}
}

func classifyInbox(content string) (inboxRoute, parsedCommand, []ruleEdit) {
	content = strings.TrimSpace(content)
	if content == "" {
		return inboxEmpty, parsedCommand{Action: "unknown", Reply: ruleHelp()}, nil
	}
	if looksLikeAck(content) {
		return inboxAck, parsedCommand{}, nil
	}
	if edits := parseRuleEdits(content); len(edits) > 0 {
		return inboxEdits, parsedCommand{}, edits
	}
	cmd := parseMailCommand(content)
	if cmd.Action != "unknown" {
		return inboxCommand, cmd, nil
	}
	if looksLikeInsight(content) {
		return inboxInsight, cmd, nil
	}
	return inboxInterpret, cmd, nil
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

func parseRuleEdits(text string) []ruleEdit {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil
	}
	var edits []ruleEdit
	if m := reRemoveNums.FindStringSubmatch(s); m != nil {
		idxs := parseIndexList(m[1])
		if len(idxs) > 0 {
			edits = append(edits, ruleEdit{Action: "remove_n", Indexes: idxs})
		}
	}
	for _, m := range reColorNum.FindAllStringSubmatch(s, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			continue
		}
		if _, _, ok := parseColorName(m[2]); !ok {
			continue
		}
		edits = append(edits, ruleEdit{Action: "color", Index: n, ColorName: m[2]})
	}
	return edits
}

func parseIndexList(s string) []int {
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
	for _, suf := range []string{" emails again", " email again", " emails", " email", " again"} {
		if strings.HasSuffix(strings.ToLower(s), suf) {
			s = strings.TrimSpace(s[:len(s)-len(suf)])
		}
	}
	s = strings.Trim(s, `"'`)
	return strings.TrimSpace(s)
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

func looksLikeHelpRequest(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	if cmdHelp.MatchString(low) {
		return true
	}
	return containsAny(low, "what can you do", "how do i use", "what commands", "how does this work")
}

func looksLikeJobPreference(s string) bool {
	low := strings.ToLower(s)
	return containsAny(low, "job alert", "job alerts", "new grad", "early career", "full time role", "full-time")
}

func jobPreferenceCriteria(s string) string {
	c := parseMailCommand(s)
	if c.Action == ruleJobFilter && strings.TrimSpace(c.Pattern) != "" {
		return strings.TrimSpace(c.Pattern)
	}
	return ""
}

func ruleHelp() string {
	return "I didn't catch that. Say `help`."
}

func ruleHelpFull() string {
	return strings.TrimSpace(`
Here's what I can do:
• Plain language — tell me what mail matters or what to skip
• ` + "`rules`" + ` — list your rules
• ` + "`remove 2`" + ` / ` + "`remove 3, 4, 5`" + ` / ` + "`forget extern`" + ` / ` + "`unmute factor75`" + ` — drop a rule
• ` + "`clear rules`" + ` — wipe them all
• ` + "`always show osprey`" + ` — force-keep matching mail
• ` + "`mute factor75`" + ` — hide a sender
• ` + "`jobs ML, backend`" + ` — only mention matching job mail
• ` + "`make it purple`" + ` / ` + "`make 2 blue`" + ` / ` + "`change rule 2 to blue`" + ` / ` + "`hyundai is green`" + ` — color that rule's digest embeds
• Combine them — ` + "`remove 3, 4, 5 and change rule 2 to blue`" + `
• Ask about mail — ` + "`did I get any Hyundai emails today?`" + ` or ` + "`anything about the car?`" + `
Colors: ` + listedColors() + `
New keep rules start grey until you pick a color.
`)
}

func applyRules(messages []ingestedMessage, facts []messageFacts, rules []mailRule) (kept []messageFacts, noise int) {
	jobTokens := jobFilterTokens(rules)
	for i, msg := range messages {
		f := messageFacts{Kind: kindNotice}
		if i < len(facts) {
			f = facts[i]
		}
		if alwaysShows(rules, msg) {
			f.Kind = kindNotice
			if strings.TrimSpace(f.Title) == "" {
				f.Title = collapseSpace(msg.subject)
			}
			if strings.TrimSpace(f.Summary) == "" {
				f.Summary = fallbackLine(msg)
			}
			f.Color = colorForMail(rules, msg, f)
			kept = append(kept, f)
			continue
		}
		if msg.replyToMe {
			f = keepReplyToMe(msg, f)
			f.Color = colorForMail(rules, msg, f)
			kept = append(kept, f)
			continue
		}
		if muted(rules, msg) {
			noise++
			continue
		}
		if f.Kind == kindPromo || f.Kind == "" {
			noise++
			continue
		}
		if len(jobTokens) > 0 && looksJobish(f.What+" "+f.Title+" "+msg.subject+" "+msg.from) && !matchesJobFilter(f, msg, jobTokens) {
			noise++
			continue
		}
		if strings.TrimSpace(f.Title) == "" && strings.TrimSpace(f.Summary) == "" && compileLine(f) == "" {
			noise++
			continue
		}
		if strings.TrimSpace(f.Title) == "" {
			f.Title = collapseSpace(msg.subject)
		}
		if strings.TrimSpace(f.Summary) == "" {
			f.Summary = compileLine(f)
		}
		f.Color = colorForMail(rules, msg, f)
		kept = append(kept, f)
	}
	return kept, noise
}

func muted(rules []mailRule, msg ingestedMessage) bool {
	hay := ruleHaystack(msg)
	for _, r := range rules {
		if r.Type == ruleMute && strings.Contains(hay, strings.ToLower(r.Pattern)) {
			return true
		}
	}
	return false
}

func alwaysShows(rules []mailRule, msg ingestedMessage) bool {
	hay := ruleHaystack(msg)
	for _, r := range rules {
		if r.Type == ruleAlwaysShow && strings.Contains(hay, strings.ToLower(r.Pattern)) {
			return true
		}
	}
	return false
}

func ruleHaystack(msg ingestedMessage) string {
	return strings.ToLower(msg.from + " " + msg.subject)
}

func jobFilterTokens(rules []mailRule) []string {
	var raw string
	for _, r := range rules {
		if r.Type == ruleJobFilter {
			raw = r.Pattern
		}
	}
	if raw == "" {
		return nil
	}
	stop := map[string]bool{
		"only": true, "show": true, "me": true, "new": true, "job": true, "jobs": true,
		"search": true, "ones": true, "if": true, "they": true, "match": true, "the": true,
		"a": true, "an": true, "or": true, "and": true, "for": true, "with": true,
		"criteria": true, "such": true, "that": true,
	}
	var tokens []string
	for _, part := range strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		part = strings.Trim(part, `."'`)
		if part == "" || stop[part] {
			continue
		}
		tokens = append(tokens, part)
	}
	return tokens
}

func matchesJobFilter(f messageFacts, msg ingestedMessage, tokens []string) bool {
	hay := strings.ToLower(f.What + " " + f.Who + " " + msg.subject + " " + msg.from)
	for _, t := range tokens {
		for _, n := range jobTokenNeedles(t) {
			if strings.Contains(hay, n) {
				return true
			}
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

func rulePromptAppendix(rules []mailRule) string {
	var lines []string
	for _, r := range rules {
		if strings.TrimSpace(r.Instruction) != "" {
			lines = append(lines, "- "+strings.TrimSpace(r.Instruction))
			continue
		}
		switch r.Type {
		case ruleMute:
			lines = append(lines, "- Do not mention mail matching "+r.Pattern+".")
		case ruleAlwaysShow:
			lines = append(lines, "- Always mention mail matching "+r.Pattern+".")
		case ruleJobFilter:
			lines = append(lines, "- Only mention job-search mail if it matches: "+r.Pattern+".")
		}
	}
	return strings.Join(lines, "\n")
}

func loadRules(ctx context.Context, db *sql.DB, userID string) ([]mailRule, error) {
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
	var rules []mailRule
	for rows.Next() {
		var r mailRule
		if err := rows.Scan(&r.ID, &r.Type, &r.Pattern, &r.Instruction, &r.Color); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func sanitizeUserRuleParse(p userRuleParse) userRuleParse {
	var mutes []string
	seenMute := map[string]bool{}
	for _, m := range p.Mutes {
		m = strings.TrimSpace(m)
		key := strings.ToLower(m)
		if !usableMutePattern(m) || seenMute[key] {
			continue
		}
		seenMute[key] = true
		mutes = append(mutes, m)
	}
	p.Mutes = mutes
	var ins []string
	seenIns := map[string]bool{}
	for _, s := range p.Instructions {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if !usableInstruction(s) || seenIns[key] {
			continue
		}
		seenIns[key] = true
		ins = append(ins, s)
	}
	p.Instructions = ins
	return p
}

func usableMutePattern(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 3 {
		return false
	}
	if containsAny(s,
		"job hunting", "hunting site", "job site", "this kind", "that kind",
		"kind of", "advertising", "product ads", "newsletter") {
		return false
	}
	switch s {
	case "job", "jobs", "email", "emails", "mail", "site", "sites",
		"product", "alert", "alerts", "hunting":
		return false
	}
	return true
}

func usableInstruction(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	low := strings.ToLower(s)
	if containsAny(low, "this kind", "that kind", "this type", "that type", "this sort") {
		return false
	}
	return true
}

func alreadyHasMute(rules []mailRule, pattern string) bool {
	return alreadyHasPattern(rules, ruleMute, pattern)
}

func alreadyHasPattern(rules []mailRule, typ, pattern string) bool {
	want := strings.ToLower(strings.TrimSpace(pattern))
	for _, r := range rules {
		if r.Type == typ && strings.ToLower(strings.TrimSpace(r.Pattern)) == want {
			return true
		}
	}
	return false
}

func alreadyHasInstruction(rules []mailRule, text string) bool {
	want := strings.ToLower(strings.TrimSpace(text))
	for _, r := range rules {
		if r.Type == ruleInstruction && strings.ToLower(strings.TrimSpace(r.Instruction)) == want {
			return true
		}
	}
	return false
}

func insertRule(ctx context.Context, db *sql.DB, userID, ruleType, pattern, instruction string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO user_mail_rules (user_id, rule_type, pattern, instruction)
		VALUES ($1, $2, $3, $4)
	`, userID, ruleType, pattern, nullIfEmpty(instruction))
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
	if _, err := db.ExecContext(ctx, `DELETE FROM user_mail_rules WHERE user_id = $1 AND rule_type = $2`, userID, ruleJobFilter); err != nil {
		return err
	}
	return insertRule(ctx, db, userID, ruleJobFilter, pattern, "")
}

func formatRules(rules []mailRule) string {
	if len(rules) == 0 {
		return "No rules yet."
	}
	var b strings.Builder
	b.WriteString("Your rules:")
	for i, r := range rules {
		fmt.Fprintf(&b, "\n%d. %s", i+1, ruleLabel(r))
		if ruleShowsColor(r) {
			fmt.Fprintf(&b, " · %s", colorLabel(r.Color))
		}
	}
	return b.String()
}

func ruleShowsColor(r mailRule) bool {
	switch r.Type {
	case ruleMute, ruleJobFilter:
		return false
	case ruleAlwaysShow:
		return true
	}
	return !isSkipInstruction(r)
}

func isSkipInstruction(r mailRule) bool {
	text := strings.TrimSpace(r.Instruction + " " + r.Pattern)
	if text == "" {
		return false
	}
	if looksLikeKeepPreference(text) && !looksLikeSkipPreference(text) {
		return false
	}
	return looksLikeSkipPreference(text) || containsAny(strings.ToLower(text),
		"not important", "do not mention", "don't mention", "do not show", "non important")
}

func ruleLabel(r mailRule) string {
	if strings.TrimSpace(r.Instruction) != "" {
		return strings.TrimSpace(r.Instruction)
	}
	switch r.Type {
	case ruleMute:
		return "mute " + r.Pattern
	case ruleAlwaysShow:
		return "always show " + r.Pattern
	case ruleJobFilter:
		return "only jobs matching " + r.Pattern
	default:
		return r.Type + ": " + r.Pattern
	}
}

func matchingRules(rules []mailRule, needle string) []mailRule {
	needle = strings.ToLower(collapseSpace(needle))
	if len(needle) < 2 {
		return nil
	}
	var out []mailRule
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

func removedReply(gone []mailRule) string {
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
