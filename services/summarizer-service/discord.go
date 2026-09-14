package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const discordAPI = "https://discord.com/api/v10"
const discordMsgLimit = 2000
const discordEmbedsPerMessage = 10

type discordEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Color       int    `json:"color"`
}

func sendDiscordDM(ctx context.Context, discordID, content string) error {
	token := strings.Trim(strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")), `"'`)
	if token == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN is unset")
	}
	if discordID == "" {
		return fmt.Errorf("missing discord id")
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	channelID, err := discordCreateDM(reqCtx, token, discordID)
	if err != nil {
		return err
	}
	return postDiscordChunks(reqCtx, token, channelID, content, nil)
}

func sendDiscordDigest(ctx context.Context, discordID string, payload digestPayload) error {
	token := strings.Trim(strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")), `"'`)
	if token == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN is unset")
	}
	if discordID == "" {
		return fmt.Errorf("missing discord id")
	}
	reqCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	channelID, err := discordCreateDM(reqCtx, token, discordID)
	if err != nil {
		return err
	}
	return postDiscordChunks(reqCtx, token, channelID, payload.Content, payload.Embeds)
}

func postDiscordChunks(ctx context.Context, token, channelID, content string, embeds []discordEmbed) error {
	content = strings.TrimSpace(content)
	if content == "" && len(embeds) == 0 {
		return nil
	}
	if len(embeds) == 0 {
		for _, chunk := range splitDiscordContent(content, discordMsgLimit) {
			if err := discordPostMessage(ctx, token, channelID, chunk, nil); err != nil {
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
		if err := discordPostMessage(ctx, token, channelID, text, page); err != nil {
			return err
		}
	}
	return nil
}

func pageDiscordEmbeds(embeds []discordEmbed, perPage int) [][]discordEmbed {
	if perPage <= 0 {
		perPage = discordEmbedsPerMessage
	}
	if len(embeds) == 0 {
		return nil
	}
	var pages [][]discordEmbed
	for i := 0; i < len(embeds); i += perPage {
		end := i + perPage
		if end > len(embeds) {
			end = len(embeds)
		}
		pages = append(pages, embeds[i:end])
	}
	return pages
}

func discordCreateDM(ctx context.Context, token, recipientID string) (string, error) {
	body, err := json.Marshal(map[string]string{"recipient_id": recipientID})
	if err != nil {
		return "", err
	}
	raw, err := discordDo(ctx, token, http.MethodPost, discordAPI+"/users/@me/channels", body)
	if err != nil {
		return "", err
	}
	var resp struct {
		ID    string `json:"id"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
		Code    int    `json:"code"`
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

func discordPostMessage(ctx context.Context, token, channelID, content string, embeds []discordEmbed) error {
	payload := map[string]any{}
	if strings.TrimSpace(content) != "" {
		payload["content"] = content
	}
	if len(embeds) > 0 {
		payload["embeds"] = embeds
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	raw, err := discordDo(ctx, token, http.MethodPost, discordAPI+"/channels/"+channelID+"/messages", body)
	if err != nil {
		return err
	}
	var resp struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("discord message decode: %w", err)
	}
	if resp.ID == "" {
		msg := resp.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("discord message: %s", msg)
	}
	return nil
}

func discordDo(ctx context.Context, token, method, url string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("Content-Type", "application/json")
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
