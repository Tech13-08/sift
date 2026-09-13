package main

import (
	"strings"
	"testing"
)

func TestExtractFactsApplicationOutcome(t *testing.T) {
	f, sure := extractFacts(ingestedMessage{
		from:    "updates@eprivatemail.com",
		subject: "Thank You From ZipRecruiter - Software Engineer, Tech Ops",
		body:    "Thank you for applying. We will not be moving forward.",
	})
	if !sure || f.Kind != kindNotice {
		t.Fatalf("%+v sure=%v", f, sure)
	}
	line := compileLine(f)
	if !strings.Contains(line, "rejected you") || !strings.Contains(line, "Software Engineer, Tech Ops") {
		t.Fatalf("line=%q", line)
	}
	if !strings.Contains(line, "ZipRecruiter") || strings.Contains(line, "Eprivatemail") {
		t.Fatalf("who should be the company in the subject: %q", line)
	}
	if strings.Contains(strings.ToLower(line), "thank you") {
		t.Fatalf("restated thank-you subject: %q", line)
	}
}

func TestExtractFactsTimeAskAndMoney(t *testing.T) {
	meet, sure := extractFacts(ingestedMessage{
		from:    "Osprey X <ospreyx13@gmail.com>",
		subject: "Meeting time",
		body:    "Can you do Zoom at 5pm tomorrow?",
	})
	if !sure || meet.Kind != kindNotice {
		t.Fatalf("%+v", meet)
	}
	line := compileLine(meet)
	if !strings.Contains(line, "Osprey") || !strings.Contains(line, "5pm") {
		t.Fatalf("%q", line)
	}

	pay, sure := extractFacts(ingestedMessage{
		from:    "noreply@speedpay.com",
		subject: "Hyundai Motor Finance Payment – Recurring Payment Confirmation",
		body:    "Your payment for amount $600.00 has been made to your Hyundai Motor Finance account.",
	})
	if !sure || pay.Kind != kindNotice {
		t.Fatalf("%+v", pay)
	}
	payLine := compileLine(pay)
	if !strings.Contains(payLine, "payment") || !strings.Contains(payLine, "Hyundai Motor Finance") || strings.Contains(payLine, "Speedpay") {
		t.Fatalf("who should be the account, not the mailer: %q", payLine)
	}
}

func TestExtractFactsJobBlastIsPromo(t *testing.T) {
	f, sure := extractFacts(ingestedMessage{
		from:    "HireFT Team <contact@hireft.com>",
		subject: "Companies Sending Assessments to HireFT Candidates This Week",
		body:    "These companies sent assessments to HireFT candidates. Explore their open roles and apply while they are actively hiring.",
	})
	if !sure || f.Kind != kindPromo {
		t.Fatalf("job list blast should not be a personal notice: %+v sure=%v", f, sure)
	}
}

func TestExtractFactsPromo(t *testing.T) {
	f, sure := extractFacts(ingestedMessage{
		from:    "Factor75 <news@g.factor75.com>",
		subject: "Ending Soon: Labor Day Sale",
		body:    "Save 30% off meals",
	})
	if !sure || f.Kind != kindPromo || compileLine(f) != "" {
		t.Fatalf("%+v", f)
	}
}

func TestFormatEmailForCategorizePutsBodyFirst(t *testing.T) {
	got := formatEmailForCategorize(ingestedMessage{
		from:    "Reddit <noreply@redditmail.com>",
		subject: `"Google SWE Internship Interview Invite"`,
		body:    "Someone posted in r/cscareerquestions. This is a link to their thread, not your interview.",
	})
	if !strings.Contains(got, "Read the body before the subject") {
		t.Fatal("categorize payload should tell the model to use the body")
	}
	if !strings.Contains(got, "happened to the mailbox owner") {
		t.Fatal("payload should ask owner vs someone else's post")
	}
	bodyAt := strings.Index(got, "Body:")
	subjAt := strings.Index(got, "Subject:")
	if bodyAt < 0 || subjAt < 0 || !strings.Contains(got, "not your interview") {
		t.Fatalf("missing labeled body: %q", got)
	}
}

func TestApplyBodyOutcomeUsesRejectionInBody(t *testing.T) {
	got := applyBodyOutcome(
		ingestedMessage{
			subject: "Thank You From ZipRecruiter - Software Engineer, Tech Ops",
			body:    "After reviewing your application, we have decided not to move you forward for the Software Engineer, Tech Ops position at this time.",
		},
		messageFacts{Kind: kindPromo, Who: "ZipRecruiter", What: "thanked for applying and reviewed the application"},
	)
	if got.Kind != kindNotice || !strings.Contains(got.What, "rejected you") || strings.Contains(strings.ToLower(got.What), "thanked") {
		t.Fatalf("body outcome ignored: %+v", got)
	}
}

func TestLooksLikeGreeting(t *testing.T) {
	if looksLikeGreeting("Hey, Osprey confirmed Zoom.") {
		t.Fatal("Hey is not a greeting we want")
	}
	if looksLikeGreeting("Wrapping up the workweek here.") {
		t.Fatal("caption is not a greeting")
	}
	if looksLikeGreeting("Evening drop — Osprey locked Zoom.") {
		t.Fatal("caption is not a greeting")
	}
	if !looksLikeGreeting("Good evening — Osprey locked Zoom for 5pm tomorrow.") {
		t.Fatal("Good evening should count")
	}
	if !looksLikeGreeting("Happy Friday. Hyundai confirmed $600.") {
		t.Fatal("Happy Friday should count")
	}
	if !looksLikeGreeting("Hope your night's quiet. ZipRecruiter rejected you.") {
		t.Fatal("Hope your… should count")
	}
}

func TestCleanDraftDropsAside(t *testing.T) {
	got := cleanDraft("Osprey at 5pm.\n\n(Aside: I'm starting to feel like a scheduling robot.)\n")
	if strings.Contains(got, "Aside") || strings.Contains(got, "robot") {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(got, "Osprey") {
		t.Fatalf("%q", got)
	}
}

func TestDeFirstPerson(t *testing.T) {
	got := deFirstPerson("ZipRecruiter declined my Software Engineer, Tech Ops application")
	if strings.Contains(strings.ToLower(got), " my ") || strings.HasPrefix(strings.ToLower(got), "my ") {
		t.Fatalf("still first person: %q", got)
	}
	if !strings.Contains(got, "your") {
		t.Fatalf("expected your: %q", got)
	}
}

func TestYouVoiceInsightKeepsSearcherI(t *testing.T) {
	got := youVoiceInsight("I didn't see a Durant email. Extern invited me to Wednesday's event.")
	if !strings.Contains(got, "I didn't see") {
		t.Fatalf("should keep searcher I: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "invited me") || !strings.Contains(strings.ToLower(got), "invited you") {
		t.Fatalf("mail events should use you: %q", got)
	}
}

func TestSelectAfterCullRestoresReplyToMe(t *testing.T) {
	kept := []messageFacts{
		{Title: "HireFT replied about the call", Summary: "They asked about Thursday.", ReplyToMe: true},
		{Title: "Payment received for 2025 Hyundai Kona", Summary: "Payment was received."},
	}
	got := selectAfterCull(kept, []int{2})
	if len(got) != 2 || !got[0].ReplyToMe {
		t.Fatalf("should restore reply-to-me: %+v", got)
	}
}

func TestSelectAfterCullRestoresPersonalDrops(t *testing.T) {
	kept := []messageFacts{
		{Title: "ZipRecruiter declined your Software Engineer, Tech Ops application", Summary: "They are not moving you forward."},
		{Title: "Osprey confirming meeting time", Summary: "Zoom at 5pm tomorrow."},
		{Title: "Payment confirmation for Hyundai Motor Finance account", Summary: "Recurring payment confirmation.", Who: "Hyundai Motor Finance", What: "confirmed a payment"},
		{Title: "ICICI Bank: Your Relationship Manager is Available", Summary: "A banker is assigned to you."},
		{Title: "Payment received for 2025 Hyundai Kona", Summary: "Payment was received.", Who: "Hyundai Motor Finance", What: "confirmed a payment"},
	}
	got := selectAfterCull(kept, []int{3, 5})
	if len(got) != 4 {
		t.Fatalf("should restore rejection and meeting: %+v", got)
	}
	if got[0].Title != kept[0].Title || got[1].Title != kept[1].Title {
		t.Fatalf("order/restore wrong: %+v", got)
	}
	for _, f := range got {
		if strings.Contains(f.Title, "ICICI") {
			t.Fatalf("broadcast should stay dropped: %+v", got)
		}
	}
}

func TestCollapseSamePayment(t *testing.T) {
	got := collapseRelatedFacts([]messageFacts{
		{
			Kind:    kindNotice,
			Who:     "Hyundai Motor Finance",
			Title:   "Payment confirmation for Hyundai Motor Finance account",
			Summary: "Speedpay confirmed a $600 recurring payment.",
			What:    "confirmed a payment",
		},
		{
			Kind:    kindNotice,
			Who:     "Hyundai Motor Finance",
			Title:   "Payment received for 2025 Hyundai Kona",
			Summary: "Hyundai confirmed the payment was received.",
			What:    "confirmed a payment",
		},
		{
			Kind:    kindNotice,
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
		messageFacts{
			Who:     "Hyundai Motor Finance",
			Title:   "Payment confirmation for Hyundai Motor Finance account",
			Summary: "Your payment of $600.00 for your Hyundai Motor Finance account has been successfully made. You can review your account information on the HMFUSA website.",
		},
		messageFacts{
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
	got := collapseRelatedFacts([]messageFacts{
		{Kind: kindNotice, Who: "Hyundai Motor Finance", Title: "Payment confirmation for Hyundai Motor Finance account", Summary: "Speedpay confirmed $600.", What: "confirmed a payment"},
		{Kind: kindNotice, Who: "bank", Title: "Payment received for 2025 Hyundai Kona", Summary: "Hyundai confirmed it was received.", What: "confirmed a payment"},
	})
	if len(got) != 1 {
		t.Fatalf("same hyundai bill should collapse even if one who is generic: %+v", got)
	}
}

func TestCollapseDifferentPaymentsStaySeparate(t *testing.T) {
	got := collapseRelatedFacts([]messageFacts{
		{Kind: kindNotice, Who: "Hyundai Motor Finance", Title: "Payment confirmation", Summary: "Paid Hyundai.", What: "confirmed a payment"},
		{Kind: kindNotice, Who: "ICICI Bank", Title: "Payment received", Summary: "Paid ICICI.", What: "confirmed a payment"},
	})
	if len(got) != 2 {
		t.Fatalf("different bills should stay separate: %+v", got)
	}
}

func TestEmbedsFromFacts(t *testing.T) {
	got := embedsFromFacts([]messageFacts{
		{Kind: kindNotice, Title: "Osprey — Zoom at 5pm tomorrow", Summary: "They want you to confirm the meeting."},
		{Kind: kindNotice, Who: "x", What: ""},
	})
	if len(got) != 1 || got[0].Title != "Osprey — Zoom at 5pm tomorrow" || got[0].Color != defaultEmbedColor {
		t.Fatalf("%+v", got)
	}
}

func TestCompileLineAnyNotice(t *testing.T) {
	got := compileLine(messageFacts{
		Kind: kindNotice,
		Who:  "Housing",
		What: "asked you to sign the lease",
		When: "Friday",
	})
	if !strings.Contains(got, "Housing") || !strings.Contains(got, "lease") || !strings.Contains(got, "Friday") {
		t.Fatalf("%q", got)
	}
}
