package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type summarizerReply struct {
	Reply string `json:"reply"`
	Error string `json:"error,omitempty"`
}

func (s *Service) callSummarizerSlash(ctx context.Context, discordID, name, arg string) (string, error) {
	return s.postSummarizer(ctx, "/v1/slash", map[string]string{
		"discord_id": discordID,
		"name":       name,
		"arg":        arg,
	})
}

func (s *Service) callSummarizerInbox(ctx context.Context, discordID, text string) (string, error) {
	return s.postSummarizer(ctx, "/v1/inbox", map[string]string{
		"discord_id": discordID,
		"text":       text,
	})
}

func (s *Service) postSummarizer(ctx context.Context, path string, body any) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.SummarizerURL+path, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("summarizer: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("summarizer HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out summarizerReply
	if err := json.Unmarshal(raw, &out); err != nil {
		text := strings.TrimSpace(string(raw))
		if text != "" {
			return text, nil
		}
		return "", fmt.Errorf("summarizer decode: %w", err)
	}
	if out.Error != "" {
		return "", fmt.Errorf("%s", out.Error)
	}
	return out.Reply, nil
}
