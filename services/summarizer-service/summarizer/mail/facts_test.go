package mail

import (
	"strings"
	"testing"

	"sift/summarizer-service/summarizer/model"
)

func TestExtractFactsApplicationOutcome(t *testing.T) {
	f, sure := ExtractFacts(model.IngestedMessage{
		From:    "updates@eprivatemail.com",
		Subject: "Thank You From ZipRecruiter - Software Engineer, Tech Ops",
		Body:    "Thank you for applying. We will not be moving forward.",
	})
	if !sure || f.Kind != model.KindNotice {
		t.Fatalf("%+v sure=%v", f, sure)
	}
	line := CompileLine(f)
	if !strings.Contains(line, "rejected you") || !strings.Contains(line, "Software Engineer, Tech Ops") {
		t.Fatalf("line=%q", line)
	}
	if !strings.Contains(line, "ZipRecruiter") || strings.Contains(line, "Eprivatemail") {
		t.Fatalf("who should be the company in the Subject: %q", line)
	}
	if strings.Contains(strings.ToLower(line), "thank you") {
		t.Fatalf("restated thank-you Subject: %q", line)
	}
}

func TestExtractFactsTimeAskAndMoney(t *testing.T) {
	meet, sure := ExtractFacts(model.IngestedMessage{
		From:    "Osprey X <ospreyx13@gmail.com>",
		Subject: "Meeting time",
		Body:    "Can you do Zoom at 5pm tomorrow?",
	})
	if !sure || meet.Kind != model.KindNotice {
		t.Fatalf("%+v", meet)
	}
	line := CompileLine(meet)
	if !strings.Contains(line, "Osprey") || !strings.Contains(line, "5pm") {
		t.Fatalf("%q", line)
	}

	pay, sure := ExtractFacts(model.IngestedMessage{
		From:    "noreply@speedpay.com",
		Subject: "Hyundai Motor Finance Payment – Recurring Payment Confirmation",
		Body:    "Your payment for amount $600.00 has been made to your Hyundai Motor Finance account.",
	})
	if !sure || pay.Kind != model.KindNotice {
		t.Fatalf("%+v", pay)
	}
	payLine := CompileLine(pay)
	if !strings.Contains(payLine, "payment") || !strings.Contains(payLine, "Hyundai Motor Finance") || strings.Contains(payLine, "Speedpay") {
		t.Fatalf("who should be the account, not the mailer: %q", payLine)
	}
}

func TestExtractFactsJobBlastIsPromo(t *testing.T) {
	f, sure := ExtractFacts(model.IngestedMessage{
		From:    "HireFT Team <contact@hireft.com>",
		Subject: "Companies Sending Assessments to HireFT Candidates This Week",
		Body:    "These companies sent assessments to HireFT candidates. Explore their open roles and apply while they are actively hiring.",
	})
	if !sure || f.Kind != model.KindPromo {
		t.Fatalf("job list blast should not be a personal notice: %+v sure=%v", f, sure)
	}
}

func TestExtractFactsPromo(t *testing.T) {
	f, sure := ExtractFacts(model.IngestedMessage{
		From:    "Factor75 <news@g.factor75.com>",
		Subject: "Ending Soon: Labor Day Sale",
		Body:    "Save 30% off meals",
	})
	if !sure || f.Kind != model.KindPromo || CompileLine(f) != "" {
		t.Fatalf("%+v", f)
	}
}

func TestApplyBodyOutcomeUsesRejectionInBody(t *testing.T) {
	got := ApplyBodyOutcome(
		model.IngestedMessage{
			Subject: "Thank You From ZipRecruiter - Software Engineer, Tech Ops",
			Body:    "After reviewing your application, we have decided not to move you forward for the Software Engineer, Tech Ops position at this time.",
		},
		model.MessageFacts{Kind: model.KindPromo, Who: "ZipRecruiter", What: "thanked for applying and reviewed the application"},
	)
	if got.Kind != model.KindNotice || !strings.Contains(got.What, "rejected you") || strings.Contains(strings.ToLower(got.What), "thanked") {
		t.Fatalf("body outcome ignored: %+v", got)
	}
}

func TestCompileLineAnyNotice(t *testing.T) {
	got := CompileLine(model.MessageFacts{
		Kind: model.KindNotice,
		Who:  "Housing",
		What: "asked you to sign the lease",
		When: "Friday",
	})
	if !strings.Contains(got, "Housing") || !strings.Contains(got, "lease") || !strings.Contains(got, "Friday") {
		t.Fatalf("%q", got)
	}
}
