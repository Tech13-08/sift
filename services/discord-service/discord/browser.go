package discord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

type digestBrowser struct {
	ID            string
	DiscordUserID string
	Order         []string
	Views         map[string]DigestPayload
	Active        string
	ChannelID     string
	RootMessageID string
	ExtraIDs      []string
	Created       time.Time
}

var (
	digestBrowsersMu sync.Mutex
	digestBrowsers   = map[string]*digestBrowser{}
)

func rememberDigestBrowser(b *digestBrowser) {
	digestBrowsersMu.Lock()
	defer digestBrowsersMu.Unlock()
	now := time.Now()
	for id, old := range digestBrowsers {
		if now.Sub(old.Created) > 7*24*time.Hour {
			delete(digestBrowsers, id)
		}
	}
	digestBrowsers[b.ID] = b
}

func getDigestBrowser(id string) *digestBrowser {
	digestBrowsersMu.Lock()
	defer digestBrowsersMu.Unlock()
	b := digestBrowsers[id]
	if b == nil {
		return nil
	}
	if time.Since(b.Created) > 7*24*time.Hour {
		delete(digestBrowsers, id)
		return nil
	}
	return b
}

func newDigestBrowserID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func mailboxButtonLabel(mailbox string) string {
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		return "inbox"
	}
	if i := strings.IndexByte(mailbox, '@'); i > 0 && i <= 70 {
		return mailbox[:i]
	}
	return clipRunes(mailbox, 70)
}

func mailboxActionRows(browserID string, order []string, active string) []map[string]any {
	var buttons []map[string]any
	for i, mb := range order {
		style := 2
		if mb == active {
			style = 1
		}
		buttons = append(buttons, map[string]any{
			"type":      2,
			"style":     style,
			"label":     mailboxButtonLabel(mb),
			"custom_id": fmt.Sprintf("sift:mb:%s:%d", browserID, i),
		})
	}
	var rows []map[string]any
	for i := 0; i < len(buttons); i += 5 {
		end := i + 5
		if end > len(buttons) {
			end = len(buttons)
		}
		rows = append(rows, map[string]any{
			"type":       1,
			"components": buttons[i:end],
		})
	}
	return rows
}

func (s *Service) sendDigestViews(ctx context.Context, discordID string, order []string, views map[string]DigestPayload) error {
	if len(order) == 0 {
		return nil
	}
	if len(order) == 1 {
		return s.sendDigest(ctx, discordID, views[order[0]])
	}
	channelID, err := s.createDM(ctx, discordID)
	if err != nil {
		return err
	}
	browserID := newDigestBrowserID()
	active := order[0]
	b := &digestBrowser{
		ID:            browserID,
		DiscordUserID: discordID,
		Order:         append([]string{}, order...),
		Views:         views,
		Active:        active,
		ChannelID:     channelID,
		Created:       time.Now(),
	}
	rememberDigestBrowser(b)
	rootID, extras, err := s.postDigestView(ctx, channelID, views[active], mailboxActionRows(browserID, order, active))
	if err != nil {
		return err
	}
	b.RootMessageID = rootID
	b.ExtraIDs = extras
	return nil
}

func (s *Service) postDigestView(ctx context.Context, channelID string, payload DigestPayload, components []map[string]any) (rootID string, extraIDs []string, err error) {
	content := strings.TrimSpace(payload.Content)
	embeds := payload.Embeds
	if content == "" && len(embeds) == 0 {
		embeds = []Embed{emptyDigestEmbed()}
	}
	if len(embeds) == 0 {
		chunks := splitDiscordContent(content, discordMsgLimit)
		rootID, err = s.postMessageFull(ctx, channelID, chunks[0], nil, components)
		if err != nil {
			return "", nil, err
		}
		for _, chunk := range chunks[1:] {
			id, err := s.postMessageFull(ctx, channelID, chunk, nil, nil)
			if err != nil {
				return rootID, extraIDs, err
			}
			extraIDs = append(extraIDs, id)
		}
		return rootID, extraIDs, nil
	}
	pages := pageDiscordEmbeds(embeds, discordEmbedsPerMessage)
	for i, page := range pages {
		text := ""
		switch {
		case i == 0:
			text = content
		case len(pages) > 1:
			text = fmt.Sprintf("Continued · page %d/%d", i+1, len(pages))
		}
		var comps []map[string]any
		if i == 0 {
			comps = components
		}
		id, err := s.postMessageFull(ctx, channelID, text, page, comps)
		if err != nil {
			return rootID, extraIDs, err
		}
		if i == 0 {
			rootID = id
		} else {
			extraIDs = append(extraIDs, id)
		}
	}
	return rootID, extraIDs, nil
}

func (s *Service) switchDigestBrowser(ctx context.Context, b *digestBrowser, mailbox string) error {
	payload, ok := b.Views[mailbox]
	if !ok {
		return fmt.Errorf("unknown mailbox")
	}
	b.Active = mailbox
	comps := mailboxActionRows(b.ID, b.Order, mailbox)
	for _, id := range b.ExtraIDs {
		_ = s.deleteMessage(ctx, b.ChannelID, id)
	}
	b.ExtraIDs = nil
	content := strings.TrimSpace(payload.Content)
	embeds := payload.Embeds
	if content == "" && len(embeds) == 0 {
		embeds = []Embed{emptyDigestEmbed()}
	}
	if len(embeds) == 0 {
		chunks := splitDiscordContent(content, discordMsgLimit)
		if _, err := s.editMessageFull(ctx, b.ChannelID, b.RootMessageID, chunks[0], []Embed{}, comps); err != nil {
			return err
		}
		for _, chunk := range chunks[1:] {
			id, err := s.postMessageFull(ctx, b.ChannelID, chunk, nil, nil)
			if err != nil {
				return err
			}
			b.ExtraIDs = append(b.ExtraIDs, id)
		}
		return nil
	}
	pages := pageDiscordEmbeds(embeds, discordEmbedsPerMessage)
	if _, err := s.editMessageFull(ctx, b.ChannelID, b.RootMessageID, content, pages[0], comps); err != nil {
		return err
	}
	for i := 1; i < len(pages); i++ {
		text := fmt.Sprintf("Continued · page %d/%d", i+1, len(pages))
		id, err := s.postMessageFull(ctx, b.ChannelID, text, pages[i], nil)
		if err != nil {
			return err
		}
		b.ExtraIDs = append(b.ExtraIDs, id)
	}
	return nil
}

func (s *Service) handleDigestButton(ctx context.Context, discordUserID, customID string) error {
	parts := strings.Split(customID, ":")
	if len(parts) != 4 || parts[0] != "sift" || parts[1] != "mb" {
		return nil
	}
	b := getDigestBrowser(parts[2])
	if b == nil {
		return fmt.Errorf("digest expired - ask for a fresh digest")
	}
	if b.DiscordUserID != "" && b.DiscordUserID != discordUserID {
		return fmt.Errorf("not your digest")
	}
	idx := 0
	fmt.Sscanf(parts[3], "%d", &idx)
	if idx < 0 || idx >= len(b.Order) {
		return fmt.Errorf("bad mailbox button")
	}
	mb := b.Order[idx]
	log.Printf("digest button user=%s mailbox=%s", discordUserID, mb)
	return s.switchDigestBrowser(ctx, b, mb)
}
