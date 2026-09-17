package rules

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sift/summarizer-service/summarizer/insights"
	"sift/summarizer-service/summarizer/llm"
	"sift/summarizer-service/summarizer/model"
	"strings"
)

func ApplyUserCommand(ctx context.Context, db *sql.DB, u model.DigestUser, content string) (string, error) {
	userID := u.ID
	content = strings.TrimSpace(content)
	route, cmd, edits := ClassifyInbox(content)
	switch route {
	case model.InboxEmpty:
		if cmd.Reply != "" {
			return cmd.Reply, nil
		}
		return RuleHelp(), nil
	case model.InboxAck:
		return "Got it.", nil
	case model.InboxEdits:
		return applyRuleEdits(ctx, db, userID, edits)
	case model.InboxInsight:
		q := strings.TrimSpace(cmd.Reply)
		if q == "" {
			q = content
		}
		return insights.AnswerInsight(ctx, db, u, q)
	case model.InboxInterpret:
		text := strings.TrimSpace(cmd.Reply)
		if text == "" {
			text = content
		}
		return ApplyRulePreference(ctx, db, u, text)
	}
	switch cmd.Action {
	case "help":
		return RuleHelpFull(), nil
	case "list":
		return ApplyListRules(ctx, db, userID)
	case "remove_all":
		n, err := deleteAllRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "No rules to clear.", nil
		}
		return "Cleared all rules.", nil
	case "remove_n":
		rules, err := LoadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		if cmd.Index < 1 || cmd.Index > len(rules) {
			return fmt.Sprintf("No rule %d. Use `/rules` to see the list.", cmd.Index), nil
		}
		gone := rules[cmd.Index-1]
		if err := deleteRuleIDs(ctx, db, userID, []string{gone.ID}); err != nil {
			return "", err
		}
		return RemovedReply([]model.MailRule{gone}), nil
	case "remove":
		return removeMatchingRules(ctx, db, userID, cmd.Pattern)
	case "unmute":
		n, err := deleteRules(ctx, db, userID, model.RuleMute, cmd.Pattern)
		if err != nil {
			return "", err
		}
		if n > 0 {
			return "Unmuted " + cmd.Pattern + ".", nil
		}
		return removeMatchingRules(ctx, db, userID, cmd.Pattern)
	case "color":
		return applyColorCommand(ctx, db, userID, cmd)
	case model.RuleAlwaysShow:
		existing, err := LoadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		if alreadyHasPattern(existing, model.RuleAlwaysShow, cmd.Pattern) {
			return "Already watching mail matching " + cmd.Pattern + ".", nil
		}
		if err := insertRule(ctx, db, userID, model.RuleAlwaysShow, cmd.Pattern, "Always mention mail matching "+cmd.Pattern+"."); err != nil {
			return "", err
		}
		return finishKeepRule(ctx, db, userID, content, "I'll treat mail matching "+cmd.Pattern+" as important.")
	case model.RuleMute:
		existing, err := LoadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		email := NormalizeMuteEmail(cmd.Pattern)
		if email == "" {
			if strings.TrimSpace(cmd.Reply) != "" {
				return strings.TrimSpace(cmd.Reply), nil
			}
			return MuteNeedEmailHelp(), nil
		}
		if MuteAlreadyCovered(existing, email) {
			return "Already muted " + email + ".", nil
		}
		if _, err := upsertMuteMerging(ctx, db, userID, email, existing); err != nil {
			return "", err
		}
		_, _ = pruneRedundantRules(ctx, db, userID)
		if strings.HasPrefix(email, "@") {
			return "Muted " + email + ". Any From on that domain (and its subdomains) is hidden.", nil
		}
		return "Muted " + email + ". Only mail From that address is hidden.", nil
	case model.RuleJobFilter:
		if err := replaceJobFilter(ctx, db, userID, cmd.Pattern); err != nil {
			return "", err
		}
		return "Watching job mail that matches: " + cmd.Pattern + ".", nil
	}
	return RuleHelp(), nil
}

// Standing mail rule only - never treat this text as an insight search.
func ApplyRulePreference(ctx context.Context, db *sql.DB, u model.DigestUser, content string) (string, error) {
	userID := u.ID
	content = strings.TrimSpace(content)
	if content == "" {
		return "Say what to keep or skip — e.g. `skip job-site product ads` or `treat finance updates as important`.", nil
	}
	// Allow short structured commands inside /rule as well.
	if edits := ParseRuleEdits(content); len(edits) > 0 {
		return applyRuleEdits(ctx, db, userID, edits)
	}
	if cmd := ParseMailCommand(content); cmd.Action != "unknown" {
		switch cmd.Action {
		case "help":
			return RuleHelpFull(), nil
		case "list":
			return ApplyListRules(ctx, db, userID)
		case model.RuleMute:
			// Sender mute only when an email/domain is present; otherwise treat as category skip below.
			if NormalizeMuteTarget(cmd.Pattern) != "" {
				return ApplyUserCommand(ctx, db, u, content)
			}
		case "remove_all", "remove_n", "remove", "unmute", "color", model.RuleAlwaysShow, model.RuleJobFilter:
			return ApplyUserCommand(ctx, db, u, content)
		}
	}

	parsed, err := llm.InterpretUserRule(ctx, content)
	if err != nil {
		log.Printf("interpret rule: %v", err)
		return "I couldn't update that. Try `/help`.", nil
	}
	// /rule is never an insight query - ignore model saying otherwise.
	if strings.EqualFold(parsed.Reply, "insight") {
		parsed.Reply = ""
	}
	if strings.EqualFold(parsed.Reply, "help") {
		return RuleHelpFull(), nil
	}
	if LooksLikeJobPreference(content) && !LooksLikeSkipPreferenceHeuristic(content) && len(parsed.Instructions) == 0 && len(parsed.Mutes) == 0 {
		criteria := strings.TrimSpace(content)
		if p := JobPreferenceCriteria(content); p != "" {
			criteria = p
		}
		if err := replaceJobFilter(ctx, db, userID, criteria); err != nil {
			return "", err
		}
		return "Watching job mail that matches: " + criteria + ".", nil
	}
	if strings.EqualFold(parsed.Reply, "list") {
		return ApplyListRules(ctx, db, userID)
	}
	parsed = PromoteHideInstructions(ctx, parsed)
	rawMutes := append([]string{}, parsed.Mutes...)
	parsed = SanitizeUserRuleParse(parsed)
	parsed.Mutes = MutesForSave(content, parsed.Mutes)
	if IsPureHideInstruction(content) || LooksLikeSkipPreferenceHeuristic(content) {
		if len(parsed.Mutes) > 0 && !LooksLikeKeepPreference(content) {
			// Sender mute from an email in the text - don't also keep a hide instruction.
			parsed.Instructions = nil
		} else if !LooksLikeKeepPreference(content) {
			// Category skip with no sender email - keep/save as instruction.
			if len(parsed.Instructions) == 0 {
				parsed.Instructions = []string{strings.TrimSpace(content)}
			}
		}
	}
	if len(parsed.Mutes) == 0 && len(rawMutes) > 0 && len(parsed.Instructions) == 0 {
		if NormalizeMuteEmail(content) == "" {
			return MuteNeedEmailHelp(), nil
		}
	}
	existing, err := LoadRules(ctx, db, userID)
	if err != nil {
		return "", err
	}
	var removed []model.MailRule
	for _, needle := range append(append([]string{}, parsed.Unmutes...), parsed.Removes...) {
		needle = strings.TrimSpace(needle)
		if needle == "" {
			continue
		}
		gone, err := deleteMatchingRules(ctx, db, userID, needle)
		if err != nil {
			return "", err
		}
		removed = append(removed, gone...)
	}
	existing, err = LoadRules(ctx, db, userID)
	if err != nil {
		return "", err
	}
	for _, m := range parsed.Mutes {
		email := NormalizeMuteEmail(m)
		if email == "" || MuteAlreadyCovered(existing, email) {
			continue
		}
		existing, err = upsertMuteMerging(ctx, db, userID, email, existing)
		if err != nil {
			return "", err
		}
	}
	for _, ins := range parsed.Instructions {
		ins = strings.TrimSpace(ins)
		if ins == "" || alreadyHasInstruction(existing, ins) || InstructionRedundant(ctx, existing, ins) {
			continue
		}
		if err := insertRule(ctx, db, userID, model.RuleInstruction, model.ClipRunes(ins, 80), ins); err != nil {
			return "", err
		}
		existing = append(existing, model.MailRule{Type: model.RuleInstruction, Instruction: ins})
	}
	if _, err := pruneRedundantRules(ctx, db, userID); err != nil {
		return "", err
	}
	if len(parsed.Instructions) > 0 {
		reply := parsed.Reply
		if reply == "" {
			reply = "Saved."
		}
		if RuleGetsColorHint(content) {
			return finishKeepRule(ctx, db, userID, content, reply)
		}
		return strings.TrimRight(strings.TrimSpace(reply), ".") + ".", nil
	}
	if len(parsed.Mutes) > 0 && parsed.Reply == "" {
		if len(parsed.Mutes) == 1 {
			return "Muted " + parsed.Mutes[0] + ".", nil
		}
		return "Muted " + strings.Join(parsed.Mutes, ", ") + ".", nil
	}
	if parsed.Reply != "" {
		return parsed.Reply, nil
	}
	if len(removed) > 0 && len(parsed.Mutes) == 0 {
		return RemovedReply(removed), nil
	}
	if len(parsed.Mutes)+len(parsed.Unmutes)+len(parsed.Removes) == 0 {
		if LooksLikeRulePreference(content) {
			ins := strings.TrimSpace(content)
			existing, err = LoadRules(ctx, db, userID)
			if err != nil {
				return "", err
			}
			if alreadyHasInstruction(existing, ins) || InstructionRedundant(ctx, existing, ins) {
				return "That's already saved.", nil
			}
			if hi, err := ClassifyHideIntent(ctx, ins); err == nil && hi.Hide {
				for _, m := range MutesForSave(ins, hi.Mutes) {
					if MuteAlreadyCovered(existing, m) {
						continue
					}
					existing, err = upsertMuteMerging(ctx, db, userID, m, existing)
					if err != nil {
						return "", err
					}
				}
				_, _ = pruneRedundantRules(ctx, db, userID)
				saved := MutesForSave(ins, hi.Mutes)
				if len(saved) == 1 {
					return "Muted " + saved[0] + ".", nil
				}
				if len(saved) > 0 {
					return "Muted " + strings.Join(saved, ", ") + ".", nil
				}
			}
			if err := insertRule(ctx, db, userID, model.RuleInstruction, model.ClipRunes(ins, 80), ins); err != nil {
				return "", err
			}
			return "Saved.", nil
		}
		return "I couldn't turn that into a rule. Try `/help`.", nil
	}
	return "Saved.", nil
}

func applyRuleEdits(ctx context.Context, db *sql.DB, userID string, edits []model.RuleEdit) (string, error) {
	rules, err := LoadRules(ctx, db, userID)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, e := range edits {
		if e.Action != "color" {
			continue
		}
		color, name, ok := ParseColorName(e.ColorName)
		if !ok {
			parts = append(parts, "I don't know that color. Try "+listedColors()+".")
			continue
		}
		if e.Index < 1 || e.Index > len(rules) {
			parts = append(parts, fmt.Sprintf("No rule %d.", e.Index))
			continue
		}
		target := rules[e.Index-1]
		if !RuleShowsColor(target) {
			parts = append(parts, fmt.Sprintf("Rule %d hides mail, so it doesn't get a color.", e.Index))
			continue
		}
		if err := setRulesColor(ctx, db, userID, []model.MailRule{target}, color); err != nil {
			return "", err
		}
		parts = append(parts, ruleLabel(target)+" is now "+name+".")
	}
	var gone []model.MailRule
	seen := map[string]bool{}
	var missing []string
	for _, e := range edits {
		if e.Action != "remove_n" {
			continue
		}
		for _, n := range e.Indexes {
			if n < 1 || n > len(rules) {
				missing = append(missing, fmt.Sprintf("No rule %d.", n))
				continue
			}
			r := rules[n-1]
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			gone = append(gone, r)
		}
	}
	if len(gone) > 0 {
		ids := make([]string, 0, len(gone))
		for _, r := range gone {
			ids = append(ids, r.ID)
		}
		if err := deleteRuleIDs(ctx, db, userID, ids); err != nil {
			return "", err
		}
		parts = append(parts, RemovedReply(gone))
	}
	parts = append(parts, missing...)
	if len(parts) == 0 {
		return RuleHelp(), nil
	}
	return strings.Join(parts, "\n"), nil
}

func finishKeepRule(ctx context.Context, db *sql.DB, userID, content, reply string) (string, error) {
	if color, name, ok := colorMentioned(content); ok {
		rules, err := LoadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		if r, found := latestColorableRule(rules); found {
			if err := setRulesColor(ctx, db, userID, []model.MailRule{r}, color); err != nil {
				return "", err
			}
			return strings.TrimRight(strings.TrimSpace(reply), ".") + ". Colored " + name + ".", nil
		}
	}
	return WithDefaultColorHint(reply), nil
}

func removeMatchingRules(ctx context.Context, db *sql.DB, userID, needle string) (string, error) {
	gone, err := deleteMatchingRules(ctx, db, userID, needle)
	if err != nil {
		return "", err
	}
	return RemovedReply(gone), nil
}

func deleteMatchingRules(ctx context.Context, db *sql.DB, userID, needle string) ([]model.MailRule, error) {
	rules, err := LoadRules(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	gone := MatchingRules(rules, needle)
	ids := make([]string, 0, len(gone))
	for _, r := range gone {
		ids = append(ids, r.ID)
	}
	if err := deleteRuleIDs(ctx, db, userID, ids); err != nil {
		return nil, err
	}
	return gone, nil
}
