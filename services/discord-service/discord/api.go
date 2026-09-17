package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type dmRequest struct {
	DiscordID string `json:"discord_id"`
	Content   string `json:"content"`
}

type digestRequest struct {
	DiscordID string        `json:"discord_id"`
	Payload   DigestPayload `json:"payload"`
}

func (s *Service) handleDM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req dmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	req.DiscordID = strings.TrimSpace(req.DiscordID)
	req.Content = strings.TrimSpace(req.Content)
	if req.DiscordID == "" || req.Content == "" {
		http.Error(w, "discord_id and content required", http.StatusBadRequest)
		return
	}
	channelID, err := s.createDM(r.Context(), req.DiscordID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.postChunks(r.Context(), channelID, req.Content, nil); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleDigest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req digestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	req.DiscordID = strings.TrimSpace(req.DiscordID)
	if req.DiscordID == "" {
		http.Error(w, "discord_id required", http.StatusBadRequest)
		return
	}
	if err := s.sendDigest(r.Context(), req.DiscordID, req.Payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) sendDigest(ctx context.Context, discordID string, payload DigestPayload) error {
	if len(payload.MailboxOrder) > 1 && len(payload.MailboxViews) > 0 {
		return s.sendDigestViews(ctx, discordID, payload.MailboxOrder, payload.MailboxViews)
	}
	channelID, err := s.createDM(ctx, discordID)
	if err != nil {
		return err
	}
	return s.postChunks(ctx, channelID, payload.Content, payload.Embeds)
}
