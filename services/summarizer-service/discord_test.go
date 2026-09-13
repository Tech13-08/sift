package main

import "testing"

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
