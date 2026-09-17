package discord

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const listRulesDebounce = 3 * time.Second

var (
	lastListReplyMu sync.Mutex
	lastListReply   = map[string]time.Time{}
)

func (s *Service) startDMPoll(ctx context.Context) {
	log.Println("discord DM poll started")
	botID, err := s.botID(ctx)
	if err != nil {
		log.Printf("dm poll bot id: %v", err)
		return
	}
	tick := func() {
		users, err := s.loadLinkedUsers(ctx)
		if err != nil {
			log.Printf("dm poll users: %v", err)
			return
		}
		for _, u := range users {
			if err := s.pollUser(ctx, botID, u); err != nil && ctx.Err() == nil {
				log.Printf("dm poll user=%s: %v", u.ID, err)
			}
		}
	}
	tick()
	t := time.NewTicker(2 * time.Second)
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

type discordMessage struct {
	ID        string
	Content   string
	AuthorID  string
	AuthorBot bool
}

func (s *Service) pollUser(ctx context.Context, botID string, u linkedUser) error {
	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	channelID, err := s.createDM(reqCtx, u.DiscordID)
	if err != nil {
		return err
	}
	cursor, err := s.loadRuleCursor(reqCtx, u.DiscordID)
	if err != nil {
		return err
	}
	msgs, err := s.listMessages(reqCtx, channelID, cursor)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	if cursor == "" {
		return s.saveRuleCursor(reqCtx, u.DiscordID, msgs[0].ID)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.AuthorID == "" || m.AuthorID == botID || m.AuthorBot || m.AuthorID != u.DiscordID {
			if err := s.saveRuleCursor(reqCtx, u.DiscordID, m.ID); err != nil {
				return err
			}
			continue
		}
		text := strings.TrimSpace(m.Content)
		log.Printf("dm poll user=%s text=%q", u.ID, text)
		if isListCommand(text) && skipRepeatList(u.ID) {
			log.Printf("dm poll skip repeat list user=%s", u.ID)
			if err := s.saveRuleCursor(reqCtx, u.DiscordID, m.ID); err != nil {
				return err
			}
			continue
		}
		cmdCtx, cmdCancel := context.WithTimeout(ctx, 3*time.Minute)
		stopTyping := s.holdTyping(cmdCtx, channelID)
		loadingID, loadErr := s.postMessage(cmdCtx, channelID, inboxPlaceholder(text), nil)
		if loadErr != nil {
			log.Printf("dm poll loading user=%s: %v", u.ID, loadErr)
		}
		reply, err := s.callSummarizerInbox(cmdCtx, u.DiscordID, text)
		stopTyping()
		if err != nil {
			cmdCancel()
			return err
		}
		if err := s.saveRuleCursor(cmdCtx, u.DiscordID, m.ID); err != nil {
			cmdCancel()
			return err
		}
		if loadErr == nil && loadingID != "" {
			if err := s.replaceMessage(cmdCtx, channelID, loadingID, reply); err != nil {
				log.Printf("dm poll replace user=%s: %v", u.ID, err)
			}
		} else if err := s.postChunks(cmdCtx, channelID, reply, nil); err != nil {
			log.Printf("dm poll reply user=%s: %v", u.ID, err)
		}
		cmdCancel()
	}
	return nil
}

func isListCommand(text string) bool {
	low := strings.ToLower(strings.TrimSpace(text))
	return low == "rules" || low == "list rules" || low == "show rules" || low == "/rules"
}

func inboxPlaceholder(text string) string {
	low := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(low, "/query") {
		return "Looking through your mail…"
	}
	return "On it…"
}

func skipRepeatList(userID string) bool {
	lastListReplyMu.Lock()
	defer lastListReplyMu.Unlock()
	if t, ok := lastListReply[userID]; ok && time.Since(t) < listRulesDebounce {
		return true
	}
	lastListReply[userID] = time.Now()
	return false
}

func (s *Service) listMessages(ctx context.Context, channelID, after string) ([]discordMessage, error) {
	url := discordAPI + "/channels/" + channelID + "/messages?limit=25"
	if after != "" {
		url += "&after=" + after
	}
	raw, err := s.discordDo(ctx, http.MethodGet, url, nil)
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

func (s *Service) loadRuleCursor(ctx context.Context, discordID string) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT last_message_id FROM discord_rule_cursors WHERE discord_id = $1`, discordID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (s *Service) saveRuleCursor(ctx context.Context, discordID, messageID string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO discord_rule_cursors (discord_id, last_message_id)
		VALUES ($1, $2)
		ON CONFLICT (discord_id) DO UPDATE SET last_message_id = EXCLUDED.last_message_id
	`, discordID, messageID)
	return err
}
