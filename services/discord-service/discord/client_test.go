package discord

import (
	"fmt"
	"testing"
)

func TestSplitDiscordContentShort(t *testing.T) {
	got := splitDiscordContent("hello", 2000)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("%q", got)
	}
}

func TestSplitDiscordContentChunks(t *testing.T) {
	got := splitDiscordContent("abcdef", 2)
	if len(got) != 3 || got[0] != "ab" || got[2] != "ef" {
		t.Fatalf("%q", got)
	}
}

func TestPageDiscordEmbedsNoLoss(t *testing.T) {
	embeds := make([]Embed, 12)
	for i := range embeds {
		embeds[i].Title = fmt.Sprintf("item-%d", i+1)
	}
	pages := pageDiscordEmbeds(embeds, 10)
	if len(pages) != 2 || len(pages[0]) != 10 || len(pages[1]) != 2 {
		t.Fatalf("%d pages sizes=%d,%d", len(pages), len(pages[0]), len(pages[1]))
	}
	if pages[1][1].Title != "item-12" {
		t.Fatalf("lost item: %+v", pages[1])
	}
}

func TestMailboxDigestButtons(t *testing.T) {
	order := []string{"alpha@x.com", "beta@y.com"}
	rows := mailboxActionRows("abc123", order, "beta@y.com")
	if len(rows) != 1 || len(rows[0]["components"].([]map[string]any)) != 2 {
		t.Fatalf("rows=%v", rows)
	}
	btns := rows[0]["components"].([]map[string]any)
	if btns[0]["label"] != "alpha" || btns[1]["label"] != "beta" {
		t.Fatalf("labels=%v %v", btns[0]["label"], btns[1]["label"])
	}
	if btns[1]["style"] != 1 || btns[0]["style"] != 2 {
		t.Fatalf("active style: %+v %+v", btns[0], btns[1])
	}
	if btns[0]["custom_id"] != "sift:mb:abc123:0" {
		t.Fatalf("custom_id=%v", btns[0]["custom_id"])
	}
}
