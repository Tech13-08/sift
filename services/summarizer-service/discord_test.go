package main

import (
	"fmt"
	"strings"
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
	embeds := make([]discordEmbed, 12)
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

func TestOrganizeByMailbox(t *testing.T) {
	kept := []messageFacts{
		{Mailbox: "a@example.com", ReplyToMe: true, Title: "Reply A", Summary: "hi"},
		{Mailbox: "b@example.com", Claimed: true, Title: "Role B", Summary: "early career"},
		{Mailbox: "a@example.com", Title: "Payment", Summary: "paid", What: "confirmed a payment"},
		{Mailbox: "b@example.com", Claimed: true, Title: "Role C", Summary: "new grad"},
	}
	got := organizeByMailbox(kept)
	if len(got) != 4 {
		t.Fatalf("expected flat multi-inbox list, got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Title, "a@example.com") {
		t.Fatalf("expected inbox A first: %q", got[0].Title)
	}
	if !strings.Contains(got[1].Title, "a@example.com") {
		t.Fatalf("expected both A items before B: %q", got[1].Title)
	}
	if !strings.Contains(got[2].Title, "b@example.com") {
		t.Fatalf("expected inbox B after A: %q", got[2].Title)
	}
	// Single-inbox days should not stamp mailbox onto titles.
	one := organizeByMailbox([]messageFacts{
		{Mailbox: "only@example.com", Title: "Solo", Summary: "one"},
		{Mailbox: "only@example.com", Title: "Two", Summary: "two"},
	})
	for _, f := range one {
		if strings.Contains(f.Title, "only@example.com") {
			t.Fatalf("single inbox should not stamp mailbox: %q", f.Title)
		}
	}
}
