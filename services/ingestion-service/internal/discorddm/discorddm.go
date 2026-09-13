package discorddm

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
)

const api = "https://discord.com/api/v10"

func PublicAppURL() string {
	u := strings.TrimRight(strings.TrimSpace(os.Getenv("AUTH_PUBLIC_URL")), "/")
	if u == "" {
		return "http://localhost:3000"
	}
	return u
}

func DeadGmailMessage(email string) string {
	return fmt.Sprintf("Sift cannot read mail for %s. Google access expired. Relink Gmail: %s", email, PublicAppURL())
}

func Send(ctx context.Context, discordID, content string) error {
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

	channelID, err := createDM(reqCtx, token, discordID)
	if err != nil {
		return err
	}
	return postMessage(reqCtx, token, channelID, content)
}

func createDM(ctx context.Context, token, recipientID string) (string, error) {
	body, err := json.Marshal(map[string]string{"recipient_id": recipientID})
	if err != nil {
		return "", err
	}
	raw, err := do(ctx, token, http.MethodPost, api+"/users/@me/channels", body)
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

func postMessage(ctx context.Context, token, channelID, content string) error {
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}
	raw, err := do(ctx, token, http.MethodPost, api+"/channels/"+channelID+"/messages", body)
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

func do(ctx context.Context, token, method, url string, body []byte) ([]byte, error) {
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
