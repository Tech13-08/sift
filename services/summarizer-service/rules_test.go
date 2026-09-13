package main

import (
	"strings"
	"testing"
	"time"
)

func TestRulePromptAppendix(t *testing.T) {
	got := rulePromptAppendix([]mailRule{
		{Type: ruleInstruction, Instruction: "Do not mention Factor75 meal kits."},
		{Type: ruleMute, Pattern: "insomniac"},
	})
	if !strings.Contains(got, "Do not mention Factor75") || !strings.Contains(got, "insomniac") {
		t.Fatalf("%q", got)
	}
}

func TestParseMailCommand(t *testing.T) {
	m := parseMailCommand("do not show me factor emails again")
	if m.Action != ruleMute || m.Pattern != "factor" {
		t.Fatalf("%+v", m)
	}
	u := parseMailCommand("unmute factor")
	if u.Action != "unmute" || u.Pattern != "factor" {
		t.Fatalf("%+v", u)
	}
	a := parseMailCommand("always show Osprey")
	if a.Action != ruleAlwaysShow || a.Pattern != "Osprey" {
		t.Fatalf("%+v", a)
	}
	j := parseMailCommand("only show job search ones if they match ML or backend")
	if j.Action != ruleJobFilter || j.Pattern == "" {
		t.Fatalf("%+v", j)
	}
	j2 := parseMailCommand("Set job alerts for full time roles within the United States targeted towards early career/new grads")
	if j2.Action != ruleJobFilter || !strings.Contains(strings.ToLower(j2.Pattern), "early career") {
		t.Fatalf("job alerts: %+v", j2)
	}
	if looksLikeHelpRequest("Set job alerts for full time roles") {
		t.Fatal("job alerts is not help")
	}
	if parseMailCommand("rules").Action != "list" {
		t.Fatal("rules")
	}
	if r := parseMailCommand("remove 2"); r.Action != "remove_n" || r.Index != 2 {
		t.Fatalf("remove n: %+v", r)
	}
	if r := parseMailCommand("forget the Extern rule"); r.Action != "remove" || !strings.EqualFold(r.Pattern, "Extern") {
		t.Fatalf("forget named: %+v", r)
	}
	if r := parseMailCommand("stop watching Extern"); r.Action != "remove" || !strings.EqualFold(r.Pattern, "Extern") {
		t.Fatalf("stop watching: %+v", r)
	}
	if parseMailCommand("clear rules").Action != "remove_all" {
		t.Fatal("clear rules")
	}
	if parseMailCommand("help").Action != "help" {
		t.Fatal("help")
	}
	if c := parseMailCommand("make it purple"); c.Action != "color" || !strings.EqualFold(c.ColorName, "purple") {
		t.Fatalf("make it: %+v", c)
	}
	if c := parseMailCommand("make 2 blue"); c.Action != "color" || c.Index != 2 || !strings.EqualFold(c.ColorName, "blue") {
		t.Fatalf("make n: %+v", c)
	}
	if c := parseMailCommand("hyundai is green"); c.Action != "color" || !strings.EqualFold(c.Pattern, "hyundai") {
		t.Fatalf("is color: %+v", c)
	}
	if parseMailCommand("extern mail is important").Action == "color" {
		t.Fatal("important is not a color")
	}
	if c := parseMailCommand("change rule 2 to blue"); c.Action != "color" || c.Index != 2 || !strings.EqualFold(c.ColorName, "blue") {
		t.Fatalf("change rule: %+v", c)
	}
}

func TestParseRuleEditsBatch(t *testing.T) {
	edits := parseRuleEdits("remove 3,4,5 and change rule 2 to blue")
	if len(edits) != 2 {
		t.Fatalf("%+v", edits)
	}
	var removed, colored bool
	for _, e := range edits {
		if e.Action == "remove_n" {
			removed = len(e.Indexes) == 3 && e.Indexes[0] == 3 && e.Indexes[2] == 5
		}
		if e.Action == "color" && e.Index == 2 && strings.EqualFold(e.ColorName, "blue") {
			colored = true
		}
	}
	if !removed || !colored {
		t.Fatalf("%+v", edits)
	}
	if got := parseIndexList("3, 4, and 5"); len(got) != 3 || got[0] != 3 || got[2] != 5 {
		t.Fatalf("%v", got)
	}
	if len(parseRuleEdits("forget the Extern rule")) != 0 {
		t.Fatal("named remove is not a numbered edit")
	}
	if len(parseRuleEdits("make it purple")) != 0 {
		t.Fatal("make it purple is a pronoun color, not a numbered edit")
	}
	typo := parseRuleEdits("remove 3,4,5 and change ruke 2 to blue")
	if len(typo) != 2 {
		t.Fatalf("typo rule: %+v", typo)
	}
}

func TestMatchingRulesAndFormat(t *testing.T) {
	rules := []mailRule{
		{ID: "a", Type: ruleMute, Pattern: "factor75"},
		{ID: "b", Type: ruleInstruction, Pattern: "Treat Extern event updates as important", Instruction: "Treat Extern event updates as important"},
	}
	got := matchingRules(rules, "extern")
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("extern match: %+v", got)
	}
	list := formatRules(rules)
	if !strings.Contains(list, "1. mute factor75") || !strings.Contains(list, "2. Treat Extern") || !strings.Contains(list, "grey") {
		t.Fatalf("list: %q", list)
	}
	skipList := formatRules([]mailRule{
		{Type: ruleMute, Pattern: "hireft"},
		{Type: ruleInstruction, Instruction: "Treat Extern event updates as important"},
		{Type: ruleInstruction, Instruction: "Treat as not important: job hunting product ads"},
	})
	if strings.Count(skipList, "grey") != 1 || strings.Contains(skipList, "product ads ·") {
		t.Fatalf("skip instructions should have no color: %q", skipList)
	}
	if strings.Contains(list, "remove 2") || strings.Contains(list, "forget extern") {
		t.Fatalf("list should not include how-to: %q", list)
	}
	if !strings.Contains(removedReply(got), "Extern") {
		t.Fatalf("removed reply: %q", removedReply(got))
	}
}

func TestDefaultColorHintAndHelp(t *testing.T) {
	got := withDefaultColorHint("I'll treat Extern mail as important.")
	if !strings.Contains(got, "I'll treat Extern mail as important") || !strings.Contains(got, "default grey") || !strings.Contains(got, "make it purple") {
		t.Fatalf("%q", got)
	}
	skip := "I don't need email from extern, hireft, or any job hunting site where it's not a specific job alert but rather advertising the product"
	if ruleGetsColorHint(skip) || !looksLikeSkipPreference(skip) {
		t.Fatal("block rules should not ask for a color")
	}
	if !ruleGetsColorHint("extern mail is important") {
		t.Fatal("keep rules still get a color")
	}
	if ruleShowsColor(mailRule{Type: ruleInstruction, Instruction: "Treat as not important: job hunting product ads"}) {
		t.Fatal("skip instruction should not show a color")
	}
	if !ruleShowsColor(mailRule{Type: ruleInstruction, Instruction: "Treat Extern event updates as important"}) {
		t.Fatal("keep instruction should show a color")
	}
}

func TestSanitizeUserRuleParse(t *testing.T) {
	got := sanitizeUserRuleParse(userRuleParse{
		Mutes:        []string{"extern", "hireft", "job hunting site", "jobs"},
		Instructions: []string{"treat this kind of mail as not important", "Skip job-site product ads; keep specific job alerts."},
	})
	if len(got.Mutes) != 2 || got.Mutes[0] != "extern" || got.Mutes[1] != "hireft" {
		t.Fatalf("mutes=%v", got.Mutes)
	}
	if len(got.Instructions) != 1 || !strings.Contains(got.Instructions[0], "job-site") {
		t.Fatalf("instructions=%v", got.Instructions)
	}
	if usableMutePattern("job hunting site") || usableInstruction("treat this kind of mail as not important") {
		t.Fatal("vague mute/instruction should be rejected")
	}
	if !strings.Contains(ruleHelpFull(), "`help`") && !strings.Contains(ruleHelpFull(), "make it purple") {
		t.Fatalf("help missing color: %q", ruleHelpFull())
	}
	if strings.Contains(ruleHelp(), "mute factor75") {
		t.Fatalf("short help should point at help: %q", ruleHelp())
	}
}

func TestColorForMailUsesRule(t *testing.T) {
	rules := []mailRule{{Type: ruleInstruction, Pattern: "Treat Extern updates as important", Instruction: "Treat Extern updates as important", Color: namedColors["purple"]}}
	got := colorForMail(rules, ingestedMessage{from: "Extern <hi@extern.co>", subject: "Wednesday event update"}, messageFacts{})
	if got != namedColors["purple"] {
		t.Fatalf("color=%d", got)
	}
	if colorForMail(nil, ingestedMessage{subject: "random"}, messageFacts{}) != 0 {
		t.Fatal("no rule should stay unset so embed falls back to grey")
	}
}

func TestClassifyInboxRoutes(t *testing.T) {
	cases := []struct {
		in    string
		route inboxRoute
	}{
		{"", inboxEmpty},
		{"thanks", inboxAck},
		{"ok", inboxAck},
		{"got it.", inboxAck},
		{"rules", inboxCommand},
		{"help", inboxCommand},
		{"mute factor75", inboxCommand},
		{"always show Osprey", inboxCommand},
		{"make it purple", inboxCommand},
		{"hyundai is green", inboxCommand},
		{"forget the Extern rule", inboxCommand},
		{"Set job alerts for full time roles within the United States targeted towards early career/new grads", inboxCommand},
		{"remove 3,4,5 and change rule 2 to blue", inboxEdits},
		{"remove 3, 4, and 5", inboxEdits},
		{"change rule 2 to blue", inboxEdits},
		{"did I get any hyundai emails this day?", inboxInsight},
		{"did I get any email from extern today?", inboxInsight},
		{"important emails from the past 24 hours", inboxInsight},
		{"what happened in my inbox?", inboxInsight},
		{"anything about the car?", inboxInsight},
		{"I don't need email from extern, hireft, or any job hunting site where it's not a specific job alert but rather advertising the product", inboxInterpret},
		{"extern mail is important", inboxInterpret},
	}
	for _, tc := range cases {
		got, _, _ := classifyInbox(tc.in)
		if got != tc.route {
			t.Fatalf("%q: got %s want %s", tc.in, got, tc.route)
		}
	}
	if looksLikeJobPreference("I don't need a specific job alert, skip the ads") && !looksLikeSkipPreference("I don't need a specific job alert, skip the ads") {
		t.Fatal("skip + job alert should not become a job filter")
	}
}

func TestLooksLikeInsight(t *testing.T) {
	if !looksLikeInsight("did i get any hyundai emails this day?") {
		t.Fatal("hyundai question")
	}
	if looksLikeInsight("extern mail is important") {
		t.Fatal("rule should not look like insight")
	}
	if !looksLikeInsight("important emails from the past 24 hours") {
		t.Fatal("recap should look like insight")
	}
	pref := "I don't need email from extern, hireft, or any job hunting site where it's not a specific job alert but rather advertising the product"
	if looksLikeInsight(pref) || !looksLikeRulePreference(pref) {
		t.Fatal("skip-these-senders is a rule, not an inbox search")
	}
	if !looksLikeInsight("did I get any email from extern today?") {
		t.Fatal("arrival question should still be insight")
	}
}

func TestParseMailQueryRecapWindow(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 12, 23, 50, 0, 0, loc)
	day := parseMailQuery("did I get any important emails in the past 24 hours?", now, loc)
	if !day.Recap || day.Needle != "" {
		t.Fatalf("recap 24h: %+v", day)
	}
	if !day.Start.Equal(now.Add(-24*time.Hour)) {
		t.Fatalf("24h start %s", day.Start)
	}
	week := parseMailQuery("important emails from the past week", now, loc)
	if !week.Recap || week.Label != "the past week" || week.Start.Day() != 6 {
		t.Fatalf("recap week: %+v", week)
	}
	brand := parseMailQuery("did I get any hyundai emails this week?", now, loc)
	if brand.Recap || brand.Needle != "hyundai" {
		t.Fatalf("brand should not be recap: %+v", brand)
	}
}

func TestKeepRecapHit(t *testing.T) {
	if !keepRecapHit(insightHit{Kind: kindNotice, From: "Hyundai <a@b.com>"}, nil) {
		t.Fatal("notice")
	}
	if keepRecapHit(insightHit{Kind: kindPromo, From: "Deals <a@b.com>"}, nil) {
		t.Fatal("promo")
	}
	if !keepRecapHit(insightHit{
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
	q := parseMailQuery("did I get any hyundai emails this day?", now, loc)
	if q.Needle != "hyundai" {
		t.Fatalf("needle=%q", q.Needle)
	}
	if q.Start.Day() != 12 || q.End.Day() != 13 {
		t.Fatalf("range %s .. %s", q.Start, q.End)
	}
	hits := []insightHit{
		{From: "Hyundai Motor Finance <HMFUSA@servicing.hmfusa.com>", Subject: "Falak, your payment has been received."},
		{From: "Hyundai Motor Finance <HMFUSA@email.hmfusa.com>", Subject: "Protect Yourself from Fraud"},
	}
	got := formatInsight("Hyundai confirmed the $600 payment.", hits)
	if !strings.Contains(got, "$600") || !strings.Contains(got, "Sources:") || !strings.Contains(got, "Protect Yourself from Fraud") {
		t.Fatalf("%q", got)
	}
	if strings.Contains(got, "3:04") || strings.Contains(got, "7:11") || strings.Contains(strings.ToLower(got), "skipped") {
		t.Fatalf("should not list times or kept/skipped: %q", got)
	}
}

func TestFormatVectorAndInsightRAGGate(t *testing.T) {
	if formatVector([]float64{1, -0.5}) != "[1.000000,-0.500000]" {
		t.Fatalf("vector %q", formatVector([]float64{1, -0.5}))
	}
	if !looksSemanticInsight("anything about the car?") {
		t.Fatal("semantic")
	}
	prev := mailVectorsEnabled
	t.Cleanup(func() { mailVectorsEnabled = prev })
	mailVectorsEnabled = true
	if !insightWantsRAG("anything about the car?", mailQuery{Needle: "car"}, nil) {
		t.Fatal("semantic should rag")
	}
	if insightWantsRAG("did I get any empower emails today", mailQuery{Needle: "empower"}, []insightHit{
		{From: "Empower <no-reply@email.empower.com>", Subject: "monitor"},
	}) {
		t.Fatal("brand hit should skip rag")
	}
	merged := mergeInsightHits(
		[]insightHit{{ID: "a", Subject: "one"}},
		[]insightHit{{ID: "a", Subject: "dup"}, {ID: "b", Subject: "two"}},
	)
	if len(merged) != 2 || merged[1].ID != "b" {
		t.Fatalf("%+v", merged)
	}
}

func TestInsightDropsBodyOnlyMention(t *testing.T) {
	hits := []insightHit{
		{From: "Empower <no-reply@email.empower.com>", Subject: "Your daily financial monitor"},
		{From: "Meetup <info@email.meetup.com>", Subject: "Your new group is waiting", Snippet: "Empower your career at this mixer."},
	}
	strong, weak := partitionInsightHits(hits, "empower")
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

func TestApplyRulesKeepsReplyToMeEvenIfMuted(t *testing.T) {
	msgs := []ingestedMessage{
		{from: "HireFT <alerts@hireft.com>", subject: "Re: Your application", body: "Can you hop on a call Thursday?", replyToMe: true},
	}
	facts := []messageFacts{{Kind: kindPromo, Who: "HireFT"}}
	rules := []mailRule{{Type: ruleMute, Pattern: "hireft"}}
	kept, noise := applyRules(msgs, facts, rules)
	if noise != 0 || len(kept) != 1 {
		t.Fatalf("reply to owner should beat mute: kept=%+v noise=%d", kept, noise)
	}
	if !kept[0].ReplyToMe || kept[0].Kind != kindNotice {
		t.Fatalf("%+v", kept[0])
	}
}

func TestApplyRulesMuteAndJobFilter(t *testing.T) {
	msgs := []ingestedMessage{
		{from: "Factor75 <news@g.factor75.com>", subject: "Labor Day Sale"},
		{from: "Annie Collins <annie.collins@ripplematch.com>", subject: "Ripplematch: New Match with BNP Paribas - Machine Learning Engineer"},
		{from: "LinkedIn <jobs-noreply@linkedin.com>", subject: "Explore new jobs for Software engineer"},
		{from: "Osprey X <ospreyx13@gmail.com>", subject: "Meeting time", body: "Zoom at 5pm tomorrow"},
	}
	facts := []messageFacts{
		{Kind: kindPromo, Who: "Factor75"},
		{Kind: kindNotice, Who: "Annie Collins", What: "New Match with BNP Paribas - Machine Learning Engineer"},
		{Kind: kindPromo, Who: "LinkedIn"},
		{Kind: kindNotice, Who: "Osprey X", What: "wants you to confirm a Zoom", When: "5pm tomorrow"},
	}
	rules := []mailRule{
		{Type: ruleMute, Pattern: "factor"},
		{Type: ruleJobFilter, Pattern: "ML, backend"},
	}
	kept, noise := applyRules(msgs, facts, rules)
	if noise < 2 {
		t.Fatalf("expected muted/promo noise, kept=%+v noise=%d", kept, noise)
	}
	var text string
	for _, f := range kept {
		text += compileLine(f) + "\n"
	}
	if !containsAny(text, "Machine Learning", "BNP") {
		t.Fatalf("job filter dropped BNP ML: %q", text)
	}
	if containsAny(text, "Factor", "Labor Day") {
		t.Fatalf("factor leaked: %q", text)
	}
	if !containsAny(text, "Osprey") {
		t.Fatalf("meeting dropped: %q", text)
	}
}

func TestHygieneFooterSkipsPeopleAndWantedMail(t *testing.T) {
	got := hygieneFooter([]senderFreq{
		{From: "Osprey X <ospreyx13@gmail.com>", Count: 4, PromoCount: 0},
		{From: "Empower <noreply@empower.com>", Count: 12, PromoCount: 0},
		{From: "Factor75 <news@g.factor75.com>", Count: 8, PromoCount: 8},
	}, nil)
	if containsAny(got, "Osprey") {
		t.Fatalf("personal sender in footer: %q", got)
	}
	if containsAny(got, "Empower") {
		t.Fatalf("wanted finance mail in footer: %q", got)
	}
	if !containsAny(got, "Factor") || !containsAny(got, "mute factor75") {
		t.Fatalf("expected mute suggestion for promo list: %q", got)
	}
	if containsAny(strings.ToLower(got), "unsubscribe") {
		t.Fatalf("should not push unsubscribe: %q", got)
	}
}

func TestHygieneFooterSkipsMuted(t *testing.T) {
	got := hygieneFooter([]senderFreq{
		{From: "Factor75 <news@g.factor75.com>", Count: 8, PromoCount: 8},
	}, []mailRule{{Type: ruleMute, Pattern: "factor75"}})
	if got != "" {
		t.Fatalf("muted promo still nudged: %q", got)
	}
}
