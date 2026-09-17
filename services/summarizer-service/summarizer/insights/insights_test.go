package insights

import (
	"sift/summarizer-service/summarizer/model"
	"strings"
	"testing"
	"time"
)

func TestParseMailQueryRecapWindow(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 23, 50, 0, 0, loc)
	day := ParseMailQuery("did I get any important emails in the past 24 hours?", now, loc)
	if !day.Recap || day.Needle != "" {
		t.Fatalf("recap 24h: %+v", day)
	}
	if !day.Start.Equal(now.Add(-24 * time.Hour)) {
		t.Fatalf("24h start %s", day.Start)
	}
	week := ParseMailQuery("important emails from the past week", now, loc)
	if !week.Recap || week.Label != "the past week" || week.Start.Day() != 6 {
		t.Fatalf("recap week: %+v", week)
	}
	brand := ParseMailQuery("did I get any hyundai emails this week?", now, loc)
	if brand.Recap || brand.Needle != "hyundai" {
		t.Fatalf("brand should not be recap: %+v", brand)
	}
}

func TestKeepRecapHit(t *testing.T) {
	if !KeepRecapHit(model.InsightHit{Kind: model.KindNotice, From: "Hyundai <a@b.com>"}, nil) {
		t.Fatal("notice")
	}
	if KeepRecapHit(model.InsightHit{Kind: model.KindPromo, From: "Deals <a@b.com>"}, nil) {
		t.Fatal("promo")
	}
	if !KeepRecapHit(model.InsightHit{
		From: "noreply@speedpay.com", Subject: "Payment confirmation",
		Snippet: "Your payment for amount $600.00 has been made to your Hyundai Motor Finance account.",
	}, nil) {
		t.Fatal("unclassified payment should count")
	}
}

func TestParseMailQueryToday(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 13, 45, 0, 0, loc)
	q := ParseMailQuery("did I get any hyundai emails this day?", now, loc)
	if q.Needle != "hyundai" {
		t.Fatalf("needle=%q", q.Needle)
	}
	if q.Start.Day() != 12 || q.End.Day() != 13 {
		t.Fatalf("range %s .. %s", q.Start, q.End)
	}
	hits := []model.InsightHit{
		{From: "Hyundai Motor Finance <HMFUSA@servicing.hmfusa.com>", Subject: "Falak, your payment has been received."},
		{From: "Hyundai Motor Finance <HMFUSA@email.hmfusa.com>", Subject: "Protect Yourself from Fraud"},
	}
	got := FormatInsight("Hyundai confirmed the $600 payment.", hits)
	if !strings.Contains(got, "$600") || !strings.Contains(got, "Sources:") || !strings.Contains(got, "Protect Yourself from Fraud") {
		t.Fatalf("%q", got)
	}
	if strings.Contains(got, "3:04") || strings.Contains(got, "7:11") || strings.Contains(strings.ToLower(got), "skipped") {
		t.Fatalf("should not list times or kept/skipped: %q", got)
	}
}

func TestInsightDropsBodyOnlyMention(t *testing.T) {
	hits := []model.InsightHit{
		{From: "Empower <no-reply@email.empower.com>", Subject: "Your daily financial monitor"},
		{From: "Meetup <info@email.meetup.com>", Subject: "Your new group is waiting", Snippet: "Empower your career at this mixer."},
	}
	strong, weak := PartitionInsightHits(hits, "empower")
	if len(strong) != 1 || !strings.Contains(strong[0].From, "Empower") {
		t.Fatalf("strong=%+v", strong)
	}
	if len(weak) != 1 || !strings.Contains(weak[0].From, "Meetup") {
		t.Fatalf("weak=%+v", weak)
	}
	if insightMatchStrength(hits[1], "empower") != 0 {
		t.Fatal("meetup body mention should not count as the brand")
	}
}
