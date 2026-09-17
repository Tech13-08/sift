package digest

import (
	"sift/summarizer-service/summarizer/model"
	"strings"
	"testing"
	"time"
)

func TestLatestSlotOnOrBeforeSameDay(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 13, 14, 0, 0, loc)
	got := LatestSlotOnOrBefore(now, loc, 9, 0)
	want := time.Date(2026, 9, 11, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestLatestSlotOnOrBeforeYesterday(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, loc)
	got := LatestSlotOnOrBefore(now, loc, 9, 0)
	want := time.Date(2026, 9, 10, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNextSlotAfter(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, loc)
	got := NextSlotAfter(now, loc, 9, 0)
	want := time.Date(2026, 9, 12, 9, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestMajorityLocalDateEveningIsToday(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 23, 0, 0, 0, loc)
	got := MajorityLocalDate(now, loc)
	if got.Year() != 2026 || got.Month() != 9 || got.Day() != 11 {
		t.Fatalf("11pm should be today: %s", got)
	}
	if !strings.Contains(SiftedHeader(now, loc), "Friday, September 11, 2026") {
		t.Fatalf("header: %s", SiftedHeader(now, loc))
	}
}

func TestMajorityLocalDateMorningIsYesterday(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 11, 8, 0, 0, 0, loc)
	got := MajorityLocalDate(now, loc)
	if got.Year() != 2026 || got.Month() != 9 || got.Day() != 10 {
		t.Fatalf("8am should be yesterday: %s", got)
	}
}

func TestSlotDSTFallBack(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 1, 10, 0, 0, 0, loc)
	got := LatestSlotOnOrBefore(now, loc, 9, 0)
	if got.Hour() != 9 || got.Day() != 1 {
		t.Fatalf("got %s", got)
	}
}

func TestCollapseSamePayment(t *testing.T) {
	got := CollapseRelatedFacts([]model.MessageFacts{
		{
			Kind:    model.KindNotice,
			Who:     "Hyundai Motor Finance",
			Title:   "Payment confirmation for Hyundai Motor Finance account",
			Summary: "Speedpay confirmed a $600 recurring payment.",
			What:    "confirmed a payment",
		},
		{
			Kind:    model.KindNotice,
			Who:     "Hyundai Motor Finance",
			Title:   "Payment received for 2025 Hyundai Kona",
			Summary: "Hyundai confirmed the payment was received.",
			What:    "confirmed a payment",
		},
		{
			Kind:    model.KindNotice,
			Who:     "Osprey",
			Title:   "Osprey — Zoom at 5pm",
			Summary: "They want you to confirm the meeting.",
		},
	})
	if len(got) != 2 {
		t.Fatalf("same bill should collapse: %+v", got)
	}
	sum := got[0].Summary
	if got[0].Who != "Hyundai Motor Finance" || !strings.Contains(sum, "$600") || !strings.Contains(strings.ToLower(sum), "received") {
		t.Fatalf("merged payment lost a fact: %+v", got[0])
	}
	if strings.Contains(sum, "\n\n") || strings.Contains(sum, "Speedpay") {
		t.Fatalf("merged payment should be one summary without the processor: %q", sum)
	}
	if got[1].Who != "Osprey" {
		t.Fatalf("meeting should stay: %+v", got)
	}
}

func TestClumpPaymentSummaryDropsRedundantCopy(t *testing.T) {
	got := clumpPaymentSummary(
		model.MessageFacts{
			Who:     "Hyundai Motor Finance",
			Title:   "Payment confirmation for Hyundai Motor Finance account",
			Summary: "Your payment of $600.00 for your Hyundai Motor Finance account has been successfully made. You can review your account information on the HMFUSA website.",
		},
		model.MessageFacts{
			Who:     "communication preferences, ple",
			Title:   "Payment received for 2025 Hyundai Kona",
			Summary: "Hyundai Motor Finance has confirmed the receipt of a $600 payment for the 2025 Hyundai Kona. It may take up to 5-7 business days for the payment to be reflected in your bank account.",
		},
	)
	low := strings.ToLower(got)
	if !strings.Contains(got, "$600") || !strings.Contains(low, "received") || !strings.Contains(low, "kona") {
		t.Fatalf("missing core facts: %q", got)
	}
	if strings.Contains(got, "\n") || strings.Contains(low, "website") || strings.Contains(low, "hmfusa") {
		t.Fatalf("still two emails / boilerplate: %q", got)
	}
	if strings.Count(low, "payment") > 1 {
		t.Fatalf("payment restated: %q", got)
	}
	if !strings.Contains(low, "5-7 business days") {
		t.Fatalf("lost settlement hint: %q", got)
	}
}

func TestCollapseSamePaymentDespiteGenericWho(t *testing.T) {
	got := CollapseRelatedFacts([]model.MessageFacts{
		{Kind: model.KindNotice, Who: "Hyundai Motor Finance", Title: "Payment confirmation for Hyundai Motor Finance account", Summary: "Speedpay confirmed $600.", What: "confirmed a payment"},
		{Kind: model.KindNotice, Who: "bank", Title: "Payment received for 2025 Hyundai Kona", Summary: "Hyundai confirmed it was received.", What: "confirmed a payment"},
	})
	if len(got) != 1 {
		t.Fatalf("same hyundai bill should collapse even if one who is generic: %+v", got)
	}
}

func TestCollapseDifferentPaymentsStaySeparate(t *testing.T) {
	got := CollapseRelatedFacts([]model.MessageFacts{
		{Kind: model.KindNotice, Who: "Hyundai Motor Finance", Title: "Payment confirmation", Summary: "Paid Hyundai.", What: "confirmed a payment"},
		{Kind: model.KindNotice, Who: "ICICI Bank", Title: "Payment received", Summary: "Paid ICICI.", What: "confirmed a payment"},
	})
	if len(got) != 2 {
		t.Fatalf("different bills should stay separate: %+v", got)
	}
}

func TestEmbedsFromFacts(t *testing.T) {
	got := EmbedsFromFacts([]model.MessageFacts{
		{Kind: model.KindNotice, Title: "Osprey — Zoom at 5pm tomorrow", Summary: "They want you to confirm the meeting."},
		{Kind: model.KindNotice, Who: "x", What: ""},
	})
	if len(got) != 1 || got[0].Title != "Osprey — Zoom at 5pm tomorrow" || got[0].Color != model.DefaultEmbedColor {
		t.Fatalf("%+v", got)
	}
}

func TestMailboxOrderByKept(t *testing.T) {
	order := []string{"ospreyx13@gmail.com", "falaktulsi@gmail.com"}
	kept := []model.MessageFacts{
		{Mailbox: "falaktulsi@gmail.com", Title: "a"},
		{Mailbox: "falaktulsi@gmail.com", Title: "b"},
		{Mailbox: "falaktulsi@gmail.com", Title: "c"},
		{Mailbox: "ospreyx13@gmail.com", Title: "d"},
	}
	got := MailboxOrderByKept(order, kept)
	if len(got) != 2 || got[0] != "falaktulsi@gmail.com" || got[1] != "ospreyx13@gmail.com" {
		t.Fatalf("want falaktulsi first (3 kept), got %v", got)
	}
	// Equal counts keep first-seen order.
	tie := MailboxOrderByKept(order, []model.MessageFacts{
		{Mailbox: "ospreyx13@gmail.com"},
		{Mailbox: "falaktulsi@gmail.com"},
	})
	if tie[0] != "ospreyx13@gmail.com" {
		t.Fatalf("tie should keep original order, got %v", tie)
	}
}
