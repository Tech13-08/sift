package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const discordAPI = "https://discord.com/api/v10"
const discordMsgLimit = 2000
const discordEmbedsPerMessage = 10

type Embed struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color"`
}

type DigestPayload struct {
	Content      string                   `json:"content"`
	Embeds       []Embed                  `json:"embeds"`
	Summary      string                   `json:"summary,omitempty"`
	MailboxOrder []string                 `json:"mailbox_order,omitempty"`
	MailboxViews map[string]DigestPayload `json:"mailbox_views,omitempty"`
}

func emptyDigestEmbed() Embed {
	return Embed{Title: "No important emails found", Color: 0x95A5A6}
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func (s *Service) createDM(ctx context.Context, recipientID string) (string, error) {
	body, err := json.Marshal(map[string]string{"recipient_id": recipientID})
	if err != nil {
		return "", err
	}
	raw, err := s.discordDo(ctx, http.MethodPost, discordAPI+"/users/@me/channels", body)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("discord dm channel decode: %w", err)
	}
	if resp.ID == "" {
		msg := resp.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return "", fmt.Errorf("discord dm channel: %s", msg)
	}
	return resp.ID, nil
}

func (s *Service) postMessage(ctx context.Context, channelID, content string, embeds []Embed) (string, error) {
	return s.postMessageFull(ctx, channelID, content, embeds, nil)
}

func (s *Service) postMessageFull(ctx context.Context, channelID, content string, embeds []Embed, components []map[string]any) (string, error) {
	payload := map[string]any{}
	if strings.TrimSpace(content) != "" {
		payload["content"] = content
	}
	if len(embeds) > 0 {
		payload["embeds"] = embeds
	}
	if len(components) > 0 {
		payload["components"] = components
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	raw, err := s.discordDo(ctx, http.MethodPost, discordAPI+"/channels/"+channelID+"/messages", body)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("discord message decode: %w", err)
	}
	if resp.ID == "" {
		msg := resp.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return "", fmt.Errorf("discord message: %s", msg)
	}
	return resp.ID, nil
}

func (s *Service) editMessage(ctx context.Context, channelID, messageID, content string) (string, error) {
	return s.editMessageFull(ctx, channelID, messageID, content, nil, nil)
}

func (s *Service) editMessageFull(ctx context.Context, channelID, messageID, content string, embeds []Embed, components []map[string]any) (string, error) {
	payload := map[string]any{"content": content}
	if embeds != nil {
		payload["embeds"] = embeds
	}
	if components != nil {
		payload["components"] = components
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	raw, err := s.discordDo(ctx, http.MethodPatch, discordAPI+"/channels/"+channelID+"/messages/"+messageID, body)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("discord edit decode: %w", err)
	}
	if resp.ID == "" {
		msg := resp.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return "", fmt.Errorf("discord edit: %s", msg)
	}
	return resp.ID, nil
}

func (s *Service) deleteMessage(ctx context.Context, channelID, messageID string) error {
	_, err := s.discordDo(ctx, http.MethodDelete, discordAPI+"/channels/"+channelID+"/messages/"+messageID, nil)
	return err
}

func (s *Service) postChunks(ctx context.Context, channelID, content string, embeds []Embed) error {
	content = strings.TrimSpace(content)
	if content == "" && len(embeds) == 0 {
		return nil
	}
	if len(embeds) == 0 {
		for _, chunk := range splitDiscordContent(content, discordMsgLimit) {
			if _, err := s.postMessage(ctx, channelID, chunk, nil); err != nil {
				return err
			}
		}
		return nil
	}
	pages := pageDiscordEmbeds(embeds, discordEmbedsPerMessage)
	total := len(pages)
	for i, page := range pages {
		text := ""
		switch {
		case i == 0:
			text = content
		case total > 1:
			text = fmt.Sprintf("Continued · page %d/%d", i+1, total)
		}
		if _, err := s.postMessage(ctx, channelID, text, page); err != nil {
			return err
		}
	}
	return nil
}

// Edit the loading message to the first chunk; post any overflow as follow-ups.
func (s *Service) replaceMessage(ctx context.Context, channelID, loadingID, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		if loadingID != "" {
			_, _ = s.editMessage(ctx, channelID, loadingID, "Done.")
		}
		return nil
	}
	chunks := splitDiscordContent(content, discordMsgLimit)
	if loadingID != "" {
		if _, err := s.editMessage(ctx, channelID, loadingID, chunks[0]); err != nil {
			return s.postChunks(ctx, channelID, content, nil)
		}
		for _, chunk := range chunks[1:] {
			if _, err := s.postMessage(ctx, channelID, chunk, nil); err != nil {
				return err
			}
		}
		return nil
	}
	return s.postChunks(ctx, channelID, content, nil)
}

// Keep Discord's typing indicator up until stop() or ctx ends.
func (s *Service) holdTyping(ctx context.Context, channelID string) (stop func()) {
	done := make(chan struct{})
	go func() {
		_, _ = s.discordDo(ctx, http.MethodPost, discordAPI+"/channels/"+channelID+"/typing", nil)
		t := time.NewTicker(8 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
				_, _ = s.discordDo(ctx, http.MethodPost, discordAPI+"/channels/"+channelID+"/typing", nil)
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

func (s *Service) discordDo(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", "Sift (https://github.com/sift, 0.1)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func (s *Service) botID(ctx context.Context) (string, error) {
	raw, err := s.discordDo(ctx, http.MethodGet, discordAPI+"/users/@me", nil)
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

func (s *Service) applicationID(ctx context.Context) (string, error) {
	discordAppIDOnce.Do(func() {
		raw, err := s.discordDo(ctx, http.MethodGet, discordAPI+"/oauth2/applications/@me", nil)
		if err != nil {
			discordAppIDErr = err
			return
		}
		var resp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil || resp.ID == "" {
			discordAppIDErr = fmt.Errorf("application id: %s", strings.TrimSpace(string(raw)))
			return
		}
		discordAppID = resp.ID
	})
	return discordAppID, discordAppIDErr
}

var (
	discordAppIDOnce sync.Once
	discordAppID     string
	discordAppIDErr  error
)

func pageDiscordEmbeds(embeds []Embed, perPage int) [][]Embed {
	if perPage <= 0 {
		perPage = discordEmbedsPerMessage
	}
	if len(embeds) == 0 {
		return nil
	}
	var pages [][]Embed
	for i := 0; i < len(embeds); i += perPage {
		end := i + perPage
		if end > len(embeds) {
			end = len(embeds)
		}
		pages = append(pages, embeds[i:end])
	}
	return pages
}

func splitDiscordContent(content string, limit int) []string {
	if utf8.RuneCountInString(content) <= limit {
		return []string{content}
	}
	var chunks []string
	var b strings.Builder
	count := 0
	for _, r := range content {
		if count >= limit {
			chunks = append(chunks, b.String())
			b.Reset()
			count = 0
		}
		b.WriteRune(r)
		count++
	}
	if b.Len() > 0 {
		chunks = append(chunks, b.String())
	}
	return chunks
}
