package digestlog

import (
	"net/url"
	"strings"
	"testing"
)

func TestNormalizeFilterQuotes(t *testing.T) {
	if got := NormalizeFilter(`"YC"`); got != "yc" {
		t.Fatalf("%q", got)
	}
	if got := NormalizeFilter(`“YC”`); got != "yc" {
		t.Fatalf("%q", got)
	}
}

func TestFilterEntriesMatchesBody(t *testing.T) {
	Clear()
	AppendLog(
		`decide skip stage=qwen from="Jobs <noreply@x.com>" subject="New openings" why="skip via qwen: blast"`,
		`Jobs <noreply@x.com> New openings skip via qwen: blast Apply to YC W27 batch`,
	)
	AppendLog(
		`decide keep stage=rules from="Empower <no-reply@empower.com>" subject="Daily update" why="passed filters"`,
		`Empower Daily update passed filters balance is fine`,
	)
	got := FilterEntries(SnapshotEntries(10), "YC")
	if len(got) != 1 || !strings.Contains(got[0], "New openings") {
		t.Fatalf("body match: %v", got)
	}
	got = FilterEntries(SnapshotEntries(10), `"empower"`)
	if len(got) != 1 || !strings.Contains(got[0], "Empower") {
		t.Fatalf("sender match: %v", got)
	}
}

func TestFilterFromQuery(t *testing.T) {
	if got := FilterFromQuery(url.Values{"filter": {"YC"}}); got != "yc" {
		t.Fatalf("%q", got)
	}
}
