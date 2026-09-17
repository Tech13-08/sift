package llm

import (
	"encoding/json"
	"sift/summarizer-service/summarizer/model"
	"strings"
	"testing"
)

func TestParseOneEmailCategory(t *testing.T) {
	raw := `{"keep":true,"title":"ZipRecruiter rejected you for Software Engineer, Tech Ops","summary":"They will not move you forward."}`
	var cat oneEmailCategory
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &cat); err != nil {
		t.Fatal(err)
	}
	if !cat.Keep || !strings.Contains(cat.Title, "rejected you") {
		t.Fatalf("%+v", cat)
	}
}

func TestParseUserRule(t *testing.T) {
	raw := `{"instructions":["Do not mention Factor75 meal kits."],"mutes":["factor75"],"unmutes":[],"removes":[],"reply":"I'll skip Factor75."}`
	var p model.UserRuleParse
	if err := json.Unmarshal([]byte(ExtractJSON(raw)), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Instructions) != 1 || p.Mutes[0] != "factor75" {
		t.Fatalf("%+v", p)
	}
}

func TestClipBody(t *testing.T) {
	long := strings.Repeat("a", 80)
	got := ClipBody(long, 10)
	if !strings.HasSuffix(got, "\n[truncated]") {
		t.Fatalf("got %q", got)
	}
}

func TestFormatEmailForCategorizePutsBodyFirst(t *testing.T) {
	got := FormatEmailForCategorize(model.IngestedMessage{
		From:    "Reddit <noreply@redditmail.com>",
		Subject: `"Google SWE Internship Interview Invite"`,
		Body:    "Someone posted in r/cscareerquestions. This is a link to their thread, not your interview.",
	}, false)
	if !strings.Contains(got, "Read the body before the subject") {
		t.Fatal("categorize payload should tell the model to use the body")
	}
	if !strings.Contains(got, "happened to the mailbox owner") {
		t.Fatal("payload should ask owner vs someone else's post")
	}
	bodyAt := strings.Index(got, "Body:")
	subjAt := strings.Index(got, "Subject:")
	if bodyAt < 0 || subjAt < 0 || !strings.Contains(got, "not your interview") {
		t.Fatalf("missing labeled Body: %q", got)
	}
}

func TestLooksLikeGreeting(t *testing.T) {
	if LooksLikeGreeting("Hey, Osprey confirmed Zoom.") {
		t.Fatal("Hey is not a greeting we want")
	}
	if LooksLikeGreeting("Wrapping up the workweek here.") {
		t.Fatal("caption is not a greeting")
	}
	if LooksLikeGreeting("Evening drop — Osprey locked Zoom.") {
		t.Fatal("caption is not a greeting")
	}
	if !LooksLikeGreeting("Good evening — Osprey locked Zoom for 5pm tomorrow.") {
		t.Fatal("Good evening should count")
	}
	if !LooksLikeGreeting("Happy Friday. Hyundai confirmed $600.") {
		t.Fatal("Happy Friday should count")
	}
	if !LooksLikeGreeting("Hope your night's quiet. ZipRecruiter rejected you.") {
		t.Fatal("Hope your… should count")
	}
}

func TestCleanDraftDropsAside(t *testing.T) {
	got := CleanDraft("Osprey at 5pm.\n\n(Aside: I'm starting to feel like a scheduling robot.)\n")
	if strings.Contains(got, "Aside") || strings.Contains(got, "robot") {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(got, "Osprey") {
		t.Fatalf("%q", got)
	}
}

func TestDeFirstPerson(t *testing.T) {
	got := DeFirstPerson("ZipRecruiter declined my Software Engineer, Tech Ops application")
	if strings.Contains(strings.ToLower(got), " my ") || strings.HasPrefix(strings.ToLower(got), "my ") {
		t.Fatalf("still first person: %q", got)
	}
	if !strings.Contains(got, "your") {
		t.Fatalf("expected your: %q", got)
	}
}

func TestYouVoiceInsightKeepsSearcherI(t *testing.T) {
	got := YouVoiceInsight("I didn't see a Durant email. Extern invited me to Wednesday's event.")
	if !strings.Contains(got, "I didn't see") {
		t.Fatalf("should keep searcher I: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "invited me") || !strings.Contains(strings.ToLower(got), "invited you") {
		t.Fatalf("mail events should use you: %q", got)
	}
}

func TestSelectAfterCullRestoresMatchedRule(t *testing.T) {
	kept := []model.MessageFacts{
		{Title: "Schwab account notice", Summary: "Something about your brokerage.", MatchedRule: 1},
		{Title: "Random newsletter", Summary: "Someone else's post."},
	}
	got := SelectAfterCull(kept, nil)
	if len(got) != 1 || got[0].MatchedRule != 1 {
		t.Fatalf("should restore matched_rule keep: %+v", got)
	}
}

func TestSelectAfterCullRestoresApplicationAndFinance(t *testing.T) {
	kept := []model.MessageFacts{
		{Title: "Your application was sent to Quippy", Summary: "LinkedIn confirmed the application."},
		{Title: "Payment confirmation for Empower", Summary: "Payment received for your account.", Outcome: "keep via qwen: payment confirmation"},
		{Title: "Daily financial monitor update", Summary: "Empower spending update.", Outcome: "keep via qwen: financial update"},
		{Title: "Random newsletter", Summary: "Someone else's post."},
	}
	got := SelectAfterCull(kept, nil)
	if len(got) != 2 {
		t.Fatalf("should restore app+real payment only, not soft financial monitor: %+v", got)
	}
}

func TestSelectAfterCullRestoresPersonalDrops(t *testing.T) {
	kept := []model.MessageFacts{
		{Title: "ZipRecruiter declined your Software Engineer, Tech Ops application", Summary: "They are not moving you forward."},
		{Title: "Osprey confirming meeting time", Summary: "Zoom at 5pm tomorrow."},
		{Title: "Payment confirmation for Hyundai Motor Finance account", Summary: "Recurring payment confirmation.", Who: "Hyundai Motor Finance", What: "confirmed a payment"},
		{Title: "ICICI Bank: Your Relationship Manager is Available", Summary: "A banker is assigned to you."},
		{Title: "Payment received for 2025 Hyundai Kona", Summary: "Payment was received.", Who: "Hyundai Motor Finance", What: "confirmed a payment"},
	}
	got := SelectAfterCull(kept, []int{3, 5})
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

func TestFormatVectorAndInsightRAGGate(t *testing.T) {
	if FormatVector([]float64{1, -0.5}) != "[1.000000,-0.500000]" {
		t.Fatalf("vector %q", FormatVector([]float64{1, -0.5}))
	}
	if !LooksSemanticInsight("anything about the car?") {
		t.Fatal("semantic")
	}
	prev := MailVectorsEnabled
	t.Cleanup(func() { MailVectorsEnabled = prev })
	MailVectorsEnabled = true
	if !InsightWantsRAG("anything about the car?", model.MailQuery{Needle: "car"}, nil) {
		t.Fatal("semantic should rag")
	}
	if InsightWantsRAG("did I get any empower emails today", model.MailQuery{Needle: "empower"}, []model.InsightHit{
		{From: "Empower <no-reply@email.empower.com>", Subject: "monitor"},
	}) {
		t.Fatal("brand hit should skip rag")
	}
	merged := MergeInsightHits(
		[]model.InsightHit{{ID: "a", Subject: "one"}},
		[]model.InsightHit{{ID: "a", Subject: "dup"}, {ID: "b", Subject: "two"}},
	)
	if len(merged) != 2 || merged[1].ID != "b" {
		t.Fatalf("%+v", merged)
	}
}
