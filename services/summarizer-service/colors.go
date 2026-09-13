package main

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

const defaultEmbedColor = 0x95A5A6

var namedColors = map[string]int{
	"grey":   defaultEmbedColor,
	"gray":   defaultEmbedColor,
	"blue":   0x3498DB,
	"green":  0x2ECC71,
	"purple": 0x9B59B6,
	"red":    0xE74C3C,
	"orange": 0xE67E22,
	"yellow": 0xF1C40F,
	"pink":   0xE91E63,
	"teal":   0x1ABC9C,
}

var colorNamesByValue = map[int]string{
	defaultEmbedColor: "grey",
	0x3498DB:          "blue",
	0x2ECC71:          "green",
	0x9B59B6:          "purple",
	0xE74C3C:          "red",
	0xE67E22:          "orange",
	0xF1C40F:          "yellow",
	0xE91E63:          "pink",
	0x1ABC9C:          "teal",
}

func parseColorName(s string) (int, string, bool) {
	name := strings.ToLower(strings.TrimSpace(s))
	n, ok := namedColors[name]
	if !ok {
		return 0, "", false
	}
	if name == "gray" {
		name = "grey"
	}
	return n, name, true
}

func colorLabel(n int) string {
	if n == 0 {
		return "grey"
	}
	if name, ok := colorNamesByValue[n]; ok {
		return name
	}
	return "grey"
}

func embedColor(n int) int {
	if n == 0 {
		return defaultEmbedColor
	}
	return n
}

func listedColors() string {
	return "grey, blue, green, purple, red, orange, yellow, pink, teal"
}

func withDefaultColorHint(reply string) string {
	reply = strings.TrimSpace(reply)
	reply = strings.TrimRight(reply, ".")
	if reply == "" {
		reply = "Saved"
	}
	return reply + ". Using the default grey color — say `make it purple` (or blue, green) if you want that changed."
}

func colorMentioned(text string) (int, string, bool) {
	low := strings.ToLower(text)
	if cmd := parseMailCommand(text); cmd.Action == "color" {
		return parseColorName(cmd.ColorName)
	}
	for _, name := range []string{"purple", "blue", "green", "red", "orange", "yellow", "pink", "teal", "grey", "gray"} {
		if strings.Contains(low, "make it "+name) || strings.Contains(low, "paint it "+name) ||
			strings.Contains(low, "color it "+name) || strings.Contains(low, " in "+name) {
			return parseColorName(name)
		}
	}
	return 0, "", false
}

func colorForMail(rules []mailRule, msg ingestedMessage, f messageFacts) int {
	hay := strings.ToLower(msg.from + " " + msg.subject + " " + f.Who + " " + f.Title + " " + f.What)
	var fallback int
	for _, r := range rules {
		if !ruleShowsColor(r) {
			continue
		}
		if !ruleMatchesMail(r, hay) {
			continue
		}
		c := embedColor(r.Color)
		if r.Type == ruleAlwaysShow {
			return c
		}
		if fallback == 0 {
			fallback = c
		}
	}
	return fallback
}

func ruleMatchesMail(r mailRule, hay string) bool {
	p := strings.ToLower(strings.TrimSpace(r.Pattern))
	if p != "" && strings.Contains(hay, p) {
		return true
	}
	src := r.Instruction
	if src == "" {
		src = r.Pattern
	}
	for _, tok := range strings.Fields(strings.ToLower(src)) {
		tok = strings.Trim(tok, ".,;:\"'")
		if len(tok) < 4 || ruleMatchStop[tok] {
			continue
		}
		if strings.Contains(hay, tok) {
			return true
		}
	}
	return false
}

var ruleMatchStop = map[string]bool{
	"treat": true, "this": true, "that": true, "kind": true, "mail": true, "email": true,
	"emails": true, "important": true, "mention": true, "always": true, "show": true,
	"from": true, "about": true, "with": true, "updates": true, "event": true,
	"matching": true, "please": true, "watch": true, "keep": true,
}

func setRulesColor(ctx context.Context, db *sql.DB, userID string, rules []mailRule, color int) error {
	ids := make([]string, 0, len(rules))
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	return setRuleColorIDs(ctx, db, userID, ids, color)
}

func setRuleColorIDs(ctx context.Context, db *sql.DB, userID string, ids []string, color int) error {
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE user_mail_rules SET color = $3
			WHERE user_id = $1 AND id = $2::uuid
		`, userID, id, color); err != nil {
			return err
		}
	}
	return nil
}

func latestColorableRule(rules []mailRule) (mailRule, bool) {
	for i := len(rules) - 1; i >= 0; i-- {
		if ruleShowsColor(rules[i]) {
			return rules[i], true
		}
	}
	return mailRule{}, false
}

func applyColorCommand(ctx context.Context, db *sql.DB, userID string, cmd parsedCommand) (string, error) {
	_, name, ok := parseColorName(cmd.ColorName)
	if !ok {
		return "I don't know that color. Try " + listedColors() + ".", nil
	}
	color, _, _ := parseColorName(cmd.ColorName)
	rules, err := loadRules(ctx, db, userID)
	if err != nil {
		return "", err
	}
	var target []mailRule
	switch {
	case cmd.Index > 0:
		if cmd.Index > len(rules) {
			return "No rule " + strconv.Itoa(cmd.Index) + ".", nil
		}
		target = []mailRule{rules[cmd.Index-1]}
	case isPronounPattern(cmd.Pattern):
		if r, ok := latestColorableRule(rules); ok {
			target = []mailRule{r}
		}
	default:
		target = matchingRules(rules, cmd.Pattern)
		if len(target) == 0 {
			if r, ok := latestColorableRule(rules); ok && strings.TrimSpace(cmd.Pattern) == "" {
				target = []mailRule{r}
			}
		}
	}
	if len(target) == 0 {
		return "No rule matched.", nil
	}
	var colorable []mailRule
	for _, r := range target {
		if ruleShowsColor(r) {
			colorable = append(colorable, r)
		}
	}
	if len(colorable) == 0 {
		return "That rule hides mail, so it doesn't get a color.", nil
	}
	target = colorable
	if err := setRulesColor(ctx, db, userID, target, color); err != nil {
		return "", err
	}
	if len(target) == 1 {
		return ruleLabel(target[0]) + " is now " + name + ".", nil
	}
	return "Updated " + strconv.Itoa(len(target)) + " rules to " + name + ".", nil
}

func isPronounPattern(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "it", "this", "that", "them":
		return true
	default:
		return false
	}
}
