package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseOneEmailCategory(t *testing.T) {
	raw := `{"keep":true,"title":"ZipRecruiter rejected you for Software Engineer, Tech Ops","summary":"They will not move you forward."}`
	var cat oneEmailCategory
	if err := json.Unmarshal([]byte(extractJSON(raw)), &cat); err != nil {
		t.Fatal(err)
	}
	if !cat.Keep || !strings.Contains(cat.Title, "rejected you") {
		t.Fatalf("%+v", cat)
	}
}

func TestParseUserRule(t *testing.T) {
	raw := `{"instructions":["Do not mention Factor75 meal kits."],"mutes":["factor75"],"unmutes":[],"removes":[],"reply":"I'll skip Factor75."}`
	var p userRuleParse
	if err := json.Unmarshal([]byte(extractJSON(raw)), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Instructions) != 1 || p.Mutes[0] != "factor75" {
		t.Fatalf("%+v", p)
	}
}

func TestClipBody(t *testing.T) {
	long := strings.Repeat("a", 80)
	got := clipBody(long, 10)
	if !strings.HasSuffix(got, "\n[truncated]") {
		t.Fatalf("got %q", got)
	}
}
