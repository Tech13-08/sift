package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	lastListReplyMu sync.Mutex
	lastListReply   = map[string]time.Time{}
)

func startRulesInbox(ctx context.Context, db *sql.DB) {
	token := strings.Trim(strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")), `"'`)
	if token == "" {
		log.Println("DISCORD_BOT_TOKEN unset; skipping rules inbox")
		return
	}
	log.Println("rules inbox polling Discord DMs")
	botID, err := discordBotID(ctx, token)
	if err != nil {
		log.Printf("rules inbox: bot id: %v", err)
		return
	}
	tick := func() {
		users, err := loadDigestUsers(ctx, db)
		if err != nil {
			log.Printf("rules inbox users: %v", err)
			return
		}
		for _, u := range users {
			if err := pollUserRules(ctx, db, token, botID, u); err != nil && ctx.Err() == nil {
				log.Printf("rules inbox user=%s: %v", u.ID, err)
			}
		}
	}
	tick()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

func pollUserRules(ctx context.Context, db *sql.DB, token, botID string, u digestUser) error {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	channelID, err := discordCreateDM(reqCtx, token, u.DiscordID)
	if err != nil {
		return err
	}
	cursor, err := loadRuleCursor(reqCtx, db, u.DiscordID)
	if err != nil {
		return err
	}
	msgs, err := discordListMessages(reqCtx, token, channelID, cursor)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	if cursor == "" {
		return saveRuleCursor(reqCtx, db, u.DiscordID, msgs[0].ID)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.AuthorID == "" || m.AuthorID == botID || m.AuthorBot || m.AuthorID != u.DiscordID {
			if err := saveRuleCursor(reqCtx, db, u.DiscordID, m.ID); err != nil {
				return err
			}
			continue
		}
		log.Printf("rules inbox user=%s text=%q", u.ID, strings.TrimSpace(m.Content))
		if parseMailCommand(m.Content).Action == "list" && skipRepeatList(u.ID) {
			log.Printf("rules inbox skip repeat list user=%s", u.ID)
			if err := saveRuleCursor(reqCtx, db, u.DiscordID, m.ID); err != nil {
				return err
			}
			continue
		}
		cmdCtx, cmdCancel := context.WithTimeout(ctx, 3*time.Minute)
		reply, err := applyUserCommand(cmdCtx, db, u, m.Content)
		cmdCancel()
		if err != nil {
			return err
		}
		if err := sendDiscordDM(reqCtx, u.DiscordID, reply); err != nil {
			return err
		}
		if err := saveRuleCursor(reqCtx, db, u.DiscordID, m.ID); err != nil {
			return err
		}
	}
	return nil
}

func skipRepeatList(userID string) bool {
	lastListReplyMu.Lock()
	defer lastListReplyMu.Unlock()
	if t, ok := lastListReply[userID]; ok && time.Since(t) < 25*time.Second {
		return true
	}
	lastListReply[userID] = time.Now()
	return false
}

func applyUserCommand(ctx context.Context, db *sql.DB, u digestUser, content string) (string, error) {
	userID := u.ID
	if edits := parseRuleEdits(content); len(edits) > 0 {
		return applyRuleEdits(ctx, db, userID, edits)
	}
	cmd := parseMailCommand(content)
	if cmd.Action == "unknown" && looksLikeInsight(content) {
		return answerInsight(ctx, db, u, content)
	}
	switch cmd.Action {
	case "help":
		return ruleHelpFull(), nil
	case "list":
		rules, err := loadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		return formatRules(rules), nil
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
		rules, err := loadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		if cmd.Index < 1 || cmd.Index > len(rules) {
			return fmt.Sprintf("No rule %d. Say `rules` to see the list.", cmd.Index), nil
		}
		gone := rules[cmd.Index-1]
		if err := deleteRuleIDs(ctx, db, userID, []string{gone.ID}); err != nil {
			return "", err
		}
		return removedReply([]mailRule{gone}), nil
	case "remove":
		return removeMatchingRules(ctx, db, userID, cmd.Pattern)
	case "unmute":
		n, err := deleteRules(ctx, db, userID, ruleMute, cmd.Pattern)
		if err != nil {
			return "", err
		}
		if n > 0 {
			return "Unmuted " + cmd.Pattern + ".", nil
		}
		return removeMatchingRules(ctx, db, userID, cmd.Pattern)
	case "color":
		return applyColorCommand(ctx, db, userID, cmd)
	case ruleAlwaysShow:
		if err := insertRule(ctx, db, userID, ruleAlwaysShow, cmd.Pattern, "Always mention mail matching "+cmd.Pattern+"."); err != nil {
			return "", err
		}
		return finishKeepRule(ctx, db, userID, content, "I'll treat mail matching "+cmd.Pattern+" as important.")
	case ruleMute:
		if err := insertRule(ctx, db, userID, ruleMute, cmd.Pattern, "Do not mention mail matching "+cmd.Pattern+"."); err != nil {
			return "", err
		}
		return "Muted " + cmd.Pattern + ".", nil
	case ruleJobFilter:
		if err := replaceJobFilter(ctx, db, userID, cmd.Pattern); err != nil {
			return "", err
		}
		return "Watching job mail that matches: " + cmd.Pattern + ".", nil
	}

	parsed, err := interpretUserRule(ctx, content)
	if err != nil {
		log.Printf("interpret rule: %v", err)
		if cmd.Action == ruleMute {
			if err := insertRule(ctx, db, userID, ruleMute, cmd.Pattern, "Do not mention mail matching "+cmd.Pattern+"."); err != nil {
				return "", err
			}
			return "Muted " + cmd.Pattern + ".", nil
		}
		return "I couldn't update that. Say `help`.", nil
	}
	if strings.EqualFold(parsed.Reply, "insight") {
		if looksLikeRulePreference(content) {
			parsed.Reply = ""
		} else {
			return answerInsight(ctx, db, u, content)
		}
	}
	if strings.EqualFold(parsed.Reply, "help") && looksLikeHelpRequest(content) {
		return ruleHelpFull(), nil
	}
	if looksLikeJobPreference(content) && !looksLikeSkipPreference(content) && len(parsed.Instructions) == 0 && len(parsed.Mutes) == 0 {
		criteria := strings.TrimSpace(content)
		if cmd.Action == ruleJobFilter && cmd.Pattern != "" {
			criteria = cmd.Pattern
		} else if p := jobPreferenceCriteria(content); p != "" {
			criteria = p
		}
		if err := replaceJobFilter(ctx, db, userID, criteria); err != nil {
			return "", err
		}
		return "Watching job mail that matches: " + criteria + ".", nil
	}
	if strings.EqualFold(parsed.Reply, "list") {
		rules, err := loadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		return formatRules(rules), nil
	}
	parsed = sanitizeUserRuleParse(parsed)
	if looksLikeSkipPreference(content) && len(parsed.Instructions) == 0 {
		parsed.Instructions = []string{strings.TrimSpace(content)}
	}
	existing, err := loadRules(ctx, db, userID)
	if err != nil {
		return "", err
	}
	var removed []mailRule
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
	for _, m := range parsed.Mutes {
		m = strings.TrimSpace(m)
		if m == "" || alreadyHasMute(existing, m) {
			continue
		}
		if err := insertRule(ctx, db, userID, ruleMute, m, ""); err != nil {
			return "", err
		}
		existing = append(existing, mailRule{Type: ruleMute, Pattern: m})
	}
	for _, ins := range parsed.Instructions {
		ins = strings.TrimSpace(ins)
		if ins == "" || alreadyHasInstruction(existing, ins) {
			continue
		}
		if err := insertRule(ctx, db, userID, ruleInstruction, clipRunes(ins, 80), ins); err != nil {
			return "", err
		}
		existing = append(existing, mailRule{Type: ruleInstruction, Instruction: ins})
	}
	if len(parsed.Instructions) > 0 {
		reply := parsed.Reply
		if reply == "" {
			reply = "Saved."
		}
		if ruleGetsColorHint(content) {
			return finishKeepRule(ctx, db, userID, content, reply)
		}
		return strings.TrimRight(strings.TrimSpace(reply), ".") + ".", nil
	}
	if parsed.Reply != "" {
		return parsed.Reply, nil
	}
	if len(removed) > 0 && len(parsed.Mutes) == 0 {
		return removedReply(removed), nil
	}
	if len(parsed.Mutes)+len(parsed.Unmutes)+len(parsed.Removes) == 0 {
		if looksLikeRulePreference(content) {
			ins := strings.TrimSpace(content)
			if alreadyHasInstruction(existing, ins) {
				return "That's already saved.", nil
			}
			if err := insertRule(ctx, db, userID, ruleInstruction, clipRunes(ins, 80), ins); err != nil {
				return "", err
			}
			return "Saved. I'll skip that kind of mail.", nil
		}
		return ruleHelp(), nil
	}
	return "Saved.", nil
}

func applyRuleEdits(ctx context.Context, db *sql.DB, userID string, edits []ruleEdit) (string, error) {
	rules, err := loadRules(ctx, db, userID)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, e := range edits {
		if e.Action != "color" {
			continue
		}
		color, name, ok := parseColorName(e.ColorName)
		if !ok {
			parts = append(parts, "I don't know that color. Try "+listedColors()+".")
			continue
		}
		if e.Index < 1 || e.Index > len(rules) {
			parts = append(parts, fmt.Sprintf("No rule %d.", e.Index))
			continue
		}
		target := rules[e.Index-1]
		if !ruleShowsColor(target) {
			parts = append(parts, fmt.Sprintf("Rule %d hides mail, so it doesn't get a color.", e.Index))
			continue
		}
		if err := setRulesColor(ctx, db, userID, []mailRule{target}, color); err != nil {
			return "", err
		}
		parts = append(parts, ruleLabel(target)+" is now "+name+".")
	}
	var gone []mailRule
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
		parts = append(parts, removedReply(gone))
	}
	parts = append(parts, missing...)
	if len(parts) == 0 {
		return ruleHelp(), nil
	}
	return strings.Join(parts, "\n"), nil
}

func finishKeepRule(ctx context.Context, db *sql.DB, userID, content, reply string) (string, error) {
	if color, name, ok := colorMentioned(content); ok {
		rules, err := loadRules(ctx, db, userID)
		if err != nil {
			return "", err
		}
		if r, found := latestColorableRule(rules); found {
			if err := setRulesColor(ctx, db, userID, []mailRule{r}, color); err != nil {
				return "", err
			}
			return strings.TrimRight(strings.TrimSpace(reply), ".") + ". Colored " + name + ".", nil
		}
	}
	return withDefaultColorHint(reply), nil
}

func removeMatchingRules(ctx context.Context, db *sql.DB, userID, needle string) (string, error) {
	gone, err := deleteMatchingRules(ctx, db, userID, needle)
	if err != nil {
		return "", err
	}
	return removedReply(gone), nil
}

func deleteMatchingRules(ctx context.Context, db *sql.DB, userID, needle string) ([]mailRule, error) {
	rules, err := loadRules(ctx, db, userID)
	if err != nil {
		return nil, err
	}
	gone := matchingRules(rules, needle)
	ids := make([]string, 0, len(gone))
	for _, r := range gone {
		ids = append(ids, r.ID)
	}
	if err := deleteRuleIDs(ctx, db, userID, ids); err != nil {
		return nil, err
	}
	return gone, nil
}

type discordMessage struct {
	ID        string
	Content   string
	AuthorID  string
	AuthorBot bool
}

func discordBotID(ctx context.Context, token string) (string, error) {
	raw, err := discordDo(ctx, token, http.MethodGet, discordAPI+"/users/@me", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.ID == "" {
		return "", fmt.Errorf("discord @me: %s", strings.TrimSpace(string(raw)))
	}
	return resp.ID, nil
}

func discordListMessages(ctx context.Context, token, channelID, after string) ([]discordMessage, error) {
	url := discordAPI + "/channels/" + channelID + "/messages?limit=25"
	if after != "" {
		url += "&after=" + after
	}
	raw, err := discordDo(ctx, token, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID      string `json:"id"`
		Content string `json:"content"`
		Author  struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"author"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("discord messages decode: %w", err)
	}
	out := make([]discordMessage, 0, len(rows))
	for _, r := range rows {
		out = append(out, discordMessage{
			ID: r.ID, Content: r.Content, AuthorID: r.Author.ID, AuthorBot: r.Author.Bot,
		})
	}
	return out, nil
}

func loadRuleCursor(ctx context.Context, db *sql.DB, discordID string) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, `SELECT last_message_id FROM discord_rule_cursors WHERE discord_id = $1`, discordID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func saveRuleCursor(ctx context.Context, db *sql.DB, discordID, messageID string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO discord_rule_cursors (discord_id, last_message_id)
		VALUES ($1, $2)
		ON CONFLICT (discord_id) DO UPDATE SET last_message_id = EXCLUDED.last_message_id
	`, discordID, messageID)
	return err
}
