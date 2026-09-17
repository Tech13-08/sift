package digest

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

	"sift/summarizer-service/summarizer/model"
)

func discordServiceURL() string {
	u := strings.TrimRight(strings.TrimSpace(os.Getenv("DISCORD_SERVICE_URL")), "/")
	if u == "" {
		return "http://discord-service:8091"
	}
	return u
}

func sendDigestPayload(ctx context.Context, discordID string, payload model.DigestPayload) error {
	if discordID == "" {
		return fmt.Errorf("missing discord id")
	}
	if payload.Content == "" && len(payload.Embeds) == 0 && len(payload.MailboxViews) == 0 {
		return nil
	}
	return postDiscordService(ctx, "/v1/digest", map[string]any{
		"discord_id": discordID,
		"payload":    payload,
	})
}

func postDiscordService(ctx context.Context, path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, discordServiceURL()+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord-service: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord-service HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}
