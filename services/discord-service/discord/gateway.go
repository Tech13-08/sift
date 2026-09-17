package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	gatewayIntentDirectMessages            = 1 << 12
	opDispatch                             = 0
	opHeartbeat                            = 1
	opIdentify                             = 2
	opHello                                = 10
	opHeartbeatACK                         = 11
	interactionAppCommand                  = 2
	interactionMessageComponent            = 3
	interactionCallbackDeferred            = 6
	interactionCallbackDeferredChannel     = 5
)

func (s *Service) startGateway(ctx context.Context) {
	go func() {
		regCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := s.registerSlashCommands(regCtx); err != nil {
			log.Printf("slash register: %v", err)
		}
		cancel()
	}()
	go func() {
		backoff := time.Second
		for {
			if ctx.Err() != nil {
				return
			}
			err := s.runGateway(ctx)
			if ctx.Err() != nil {
				return
			}
			log.Printf("discord gateway: %v; reconnect in %s", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}()
}

func (s *Service) runGateway(ctx context.Context) error {
	url, err := s.gatewayURL(ctx)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, url+"/?v=10&encoding=json", nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	var (
		seqMu    sync.Mutex
		seq      *int
		hbCancel context.CancelFunc
	)
	stopHB := func() {
		if hbCancel != nil {
			hbCancel()
			hbCancel = nil
		}
	}
	defer stopHB()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var pkt struct {
			Op int             `json:"op"`
			D  json.RawMessage `json:"d"`
			S  *int            `json:"s"`
			T  string          `json:"t"`
		}
		if err := json.Unmarshal(data, &pkt); err != nil {
			log.Printf("gateway decode: %v", err)
			continue
		}
		if pkt.S != nil {
			seqMu.Lock()
			seq = pkt.S
			seqMu.Unlock()
		}
		switch pkt.Op {
		case opHello:
			var hello struct {
				HeartbeatInterval int `json:"heartbeat_interval"`
			}
			if err := json.Unmarshal(pkt.D, &hello); err != nil {
				return fmt.Errorf("hello: %w", err)
			}
			stopHB()
			hbCtx, cancel := context.WithCancel(ctx)
			hbCancel = cancel
			go s.heartbeat(hbCtx, writeJSON, time.Duration(hello.HeartbeatInterval)*time.Millisecond, &seqMu, &seq)
			identify := map[string]any{
				"op": opIdentify,
				"d": map[string]any{
					"token":   s.Token,
					"intents": gatewayIntentDirectMessages,
					"properties": map[string]string{
						"os": "linux", "browser": "sift", "device": "sift",
					},
				},
			}
			if err := writeJSON(identify); err != nil {
				return err
			}
			log.Println("discord gateway identified")
		case opHeartbeatACK:
		case opDispatch:
			switch pkt.T {
			case "INTERACTION_CREATE":
				go s.handleInteraction(ctx, pkt.D)
			case "READY":
				log.Println("discord gateway ready")
			}
		case opHeartbeat:
			seqMu.Lock()
			cur := seq
			seqMu.Unlock()
			_ = writeJSON(map[string]any{"op": opHeartbeat, "d": cur})
		}
	}
}

func (s *Service) heartbeat(ctx context.Context, writeJSON func(any) error, interval time.Duration, seqMu *sync.Mutex, seq **int) {
	if interval <= 0 {
		interval = 41250 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			seqMu.Lock()
			cur := *seq
			seqMu.Unlock()
			if err := writeJSON(map[string]any{"op": opHeartbeat, "d": cur}); err != nil {
				return
			}
		}
	}
}

func (s *Service) gatewayURL(ctx context.Context) (string, error) {
	raw, err := s.discordDo(ctx, http.MethodGet, discordAPI+"/gateway", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.URL == "" {
		return "", fmt.Errorf("gateway url: %s", strings.TrimSpace(string(raw)))
	}
	return resp.URL, nil
}

func (s *Service) handleInteraction(ctx context.Context, raw json.RawMessage) {
	var ix struct {
		ID    string `json:"id"`
		Token string `json:"token"`
		Type  int    `json:"type"`
		Data  struct {
			Name     string `json:"name"`
			CustomID string `json:"custom_id"`
			Options  []struct {
				Name  string `json:"name"`
				Type  int    `json:"type"`
				Value any    `json:"value"`
			} `json:"options"`
		} `json:"data"`
		User *struct {
			ID string `json:"id"`
		} `json:"user"`
		Member *struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"member"`
	}
	if err := json.Unmarshal(raw, &ix); err != nil {
		log.Printf("interaction decode: %v", err)
		return
	}
	discordUserID := ""
	if ix.User != nil {
		discordUserID = ix.User.ID
	} else if ix.Member != nil {
		discordUserID = ix.Member.User.ID
	}

	switch ix.Type {
	case interactionMessageComponent:
		if !strings.HasPrefix(ix.Data.CustomID, "sift:mb:") {
			return
		}
		ackCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := s.interactionCallback(ackCtx, ix.ID, ix.Token, map[string]any{"type": interactionCallbackDeferred}); err != nil {
			log.Printf("interaction ack: %v", err)
			return
		}
		workCtx, workCancel := context.WithTimeout(ctx, 30*time.Second)
		defer workCancel()
		if err := s.handleDigestButton(workCtx, discordUserID, ix.Data.CustomID); err != nil {
			log.Printf("digest button: %v", err)
			_ = s.interactionFollowup(workCtx, ix.Token, "Couldn't switch inboxes - ask for a fresh digest.")
		}
	case interactionAppCommand:
		ackCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := s.interactionCallback(ackCtx, ix.ID, ix.Token, map[string]any{"type": interactionCallbackDeferredChannel}); err != nil {
			log.Printf("slash ack: %v", err)
			return
		}
		workCtx, workCancel := context.WithTimeout(ctx, 3*time.Minute)
		defer workCancel()
		arg := slashOptionString(ix.Data.Options, "text")
		if arg == "" {
			arg = slashOptionString(ix.Data.Options, "email")
		}
		log.Printf("slash discord=%s cmd=/%s arg=%q", discordUserID, ix.Data.Name, arg)
		reply, err := s.callSummarizerSlash(workCtx, discordUserID, ix.Data.Name, arg)
		if err != nil {
			log.Printf("slash /%s: %v", ix.Data.Name, err)
			_ = s.interactionEditOriginal(workCtx, ix.Token, "Something went wrong - try again.")
			return
		}
		if err := s.interactionEditOriginal(workCtx, ix.Token, reply); err != nil {
			log.Printf("slash edit: %v", err)
			_ = s.interactionFollowup(workCtx, ix.Token, reply)
		}
	}
}

func slashOptionString(opts []struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Value any    `json:"value"`
}, name string) string {
	for _, o := range opts {
		if !strings.EqualFold(o.Name, name) {
			continue
		}
		switch v := o.Value.(type) {
		case string:
			return strings.TrimSpace(v)
		default:
			return strings.TrimSpace(fmt.Sprint(v))
		}
	}
	return ""
}

func (s *Service) interactionCallback(ctx context.Context, interactionID, interactionToken string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := discordAPI + "/interactions/" + interactionID + "/" + interactionToken + "/callback"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Sift (https://github.com/sift, 0.1)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("interaction callback HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (s *Service) interactionEditOriginal(ctx context.Context, interactionToken, content string) error {
	appID, err := s.applicationID(ctx)
	if err != nil {
		return err
	}
	chunks := splitDiscordContent(strings.TrimSpace(content), discordMsgLimit)
	if len(chunks) == 0 {
		chunks = []string{"Done."}
	}
	body, err := json.Marshal(map[string]any{"content": chunks[0]})
	if err != nil {
		return err
	}
	url := discordAPI + "/webhooks/" + appID + "/" + interactionToken + "/messages/@original"
	if _, err := s.discordDo(ctx, http.MethodPatch, url, body); err != nil {
		return err
	}
	for _, chunk := range chunks[1:] {
		_ = s.interactionFollowup(ctx, interactionToken, chunk)
	}
	return nil
}

func (s *Service) interactionFollowup(ctx context.Context, interactionToken, content string) error {
	appID, err := s.applicationID(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		return err
	}
	url := discordAPI + "/webhooks/" + appID + "/" + interactionToken
	_, err = s.discordDo(ctx, http.MethodPost, url, body)
	return err
}

func (s *Service) registerSlashCommands(ctx context.Context) error {
	appID, err := s.applicationID(ctx)
	if err != nil {
		return err
	}
	install := []int{0, 1}
	contexts := []int{0, 1, 2}
	cmds := []map[string]any{
		{
			"name": "rule", "description": "Keep or skip kinds of mail (topics, watches)",
			"dm_permission": true, "integration_types": install, "contexts": contexts,
			"options": []map[string]any{{"name": "text", "description": "e.g. skip job-site product ads, or treat finance updates as important", "type": 3, "required": true}},
		},
		{
			"name": "mute", "description": "Hide a sender by email or domain",
			"dm_permission": true, "integration_types": install, "contexts": contexts,
			"options": []map[string]any{{"name": "email", "description": "e.g. @extern.com or community@extern.com", "type": 3, "required": true}},
		},
		{
			"name": "query", "description": "Ask about mail in your linked inboxes",
			"dm_permission": true, "integration_types": install, "contexts": contexts,
			"options": []map[string]any{{"name": "text", "description": "e.g. did I get any Hyundai emails today?", "type": 3, "required": true}},
		},
		{"name": "rules", "description": "List your saved mail rules", "dm_permission": true, "integration_types": install, "contexts": contexts},
		{"name": "help", "description": "How to use Sift", "dm_permission": true, "integration_types": install, "contexts": contexts},
	}
	body, err := json.Marshal(cmds)
	if err != nil {
		return err
	}
	if _, err := s.discordDo(ctx, http.MethodPut, discordAPI+"/applications/"+appID+"/commands", body); err != nil {
		return fmt.Errorf("register slash commands: %w", err)
	}
	log.Println("discord slash commands registered: /rule /mute /query /rules /help (guild+user, guild+dm+private)")
	return nil
}
