package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"sift/summarizer-service/summarizer/digest"
	"sift/summarizer-service/summarizer/model"
	"sift/summarizer-service/summarizer/rules"
)

type inboxAPIRequest struct {
	DiscordID string `json:"discord_id"`
	Text      string `json:"text"`
}

type slashAPIRequest struct {
	DiscordID string `json:"discord_id"`
	Name      string `json:"name"`
	Arg       string `json:"arg"`
}

type askAPIRequest struct {
	UserID string `json:"user_id"`
	Text   string `json:"text"`
}

type apiReply struct {
	Reply string `json:"reply"`
	Error string `json:"error,omitempty"`
}

func MountCommandAPI(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("/v1/inbox", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req inboxAPIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.DiscordID = strings.TrimSpace(req.DiscordID)
		req.Text = strings.TrimSpace(req.Text)
		if req.DiscordID == "" {
			http.Error(w, "discord_id required", http.StatusBadRequest)
			return
		}
		u, err := digest.LoadUserByDiscordID(r.Context(), db, req.DiscordID)
		if err != nil {
			writeAPIReply(w, "", err)
			return
		}
		reply, err := rules.ApplyUserCommand(r.Context(), db, u, req.Text)
		writeAPIReply(w, reply, err)
	})
	mux.HandleFunc("/v1/slash", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req slashAPIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.DiscordID = strings.TrimSpace(req.DiscordID)
		req.Name = strings.TrimSpace(req.Name)
		req.Arg = strings.TrimSpace(req.Arg)
		if req.DiscordID == "" || req.Name == "" {
			http.Error(w, "discord_id and name required", http.StatusBadRequest)
			return
		}
		u, err := digest.LoadUserByDiscordID(r.Context(), db, req.DiscordID)
		if err != nil {
			writeAPIReply(w, "", err)
			return
		}
		reply, err := rules.ApplySlashCommand(r.Context(), db, u, req.Name, req.Arg)
		writeAPIReply(w, reply, err)
	})
	mux.HandleFunc("/v1/ask", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req askAPIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		req.UserID = strings.TrimSpace(req.UserID)
		req.Text = strings.TrimSpace(req.Text)
		if req.UserID == "" || req.Text == "" {
			http.Error(w, "user_id and text required", http.StatusBadRequest)
			return
		}
		u, err := digest.LoadUserByID(r.Context(), db, req.UserID)
		if err != nil {
			writeAPIReply(w, "", err)
			return
		}
		reply, err := rules.ApplySlashCommand(r.Context(), db, u, model.SlashQuery, req.Text)
		writeAPIReply(w, reply, err)
	})
}

func writeAPIReply(w http.ResponseWriter, reply string, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(apiReply{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(apiReply{Reply: reply})
}
