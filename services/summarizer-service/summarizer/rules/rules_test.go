package rules

import (
	"context"
	"strings"
	"testing"

	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
)

func TestRulePromptAppendix(t *testing.T) {
	got := RulePromptAppendix([]model.MailRule{
		{Type: model.RuleInstruction, Instruction: "treat emails about finance update as important", Color: NamedColors["green"]},
		{Type: model.RuleInstruction, Instruction: "Do not mention Factor75 meal kits."},
		{Type: model.RuleMute, Pattern: "insomniac"},
		{Type: model.RuleJobFilter, Pattern: "ML, backend", Color: NamedColors["blue"]},
	})
	if !strings.Contains(got, "[1] treat emails about finance update") {
		t.Fatalf("missing numbered keep rule: %q", got)
	}
	if !strings.Contains(got, "[2] Only mention job-search mail if it matches: ML, backend") {
		t.Fatalf("missing numbered job filter: %q", got)
	}
	if !strings.Contains(got, "Do not mention Factor75") || !strings.Contains(got, "insomniac") {
		t.Fatalf("missing skip lines: %q", got)
	}
	if !strings.Contains(got, "matched_rule") {
		t.Fatalf("missing matched_rule guidance: %q", got)
	}
}

func TestColorForFactUsesMatchedRule(t *testing.T) {
	mailRules := []model.MailRule{
		{Type: model.RuleInstruction, Instruction: "keep emails about real personal financial transactions", Color: NamedColors["green"]},
		{Type: model.RuleInstruction, Instruction: "skip job-site product ads"},
		{Type: model.RuleJobFilter, Pattern: "early career", Color: NamedColors["blue"]},
	}
	// High-confidence match → green.
	donation := model.IngestedMessage{
		From: "Tremendous <rewards@reward.tremendous.com>", Subject: "Thank you for your $5.00 USD donation",
		Body: "You donated $5.00 USD",
	}
	got := ColorForFact(mailRules, donation, model.MessageFacts{
		MatchedRule: 1, RuleConfidence: 90, Kind: model.KindNotice,
		Title: "Donation confirmation", Summary: "You donated $5.00",
	})
	if got != NamedColors["green"] {
		t.Fatalf("confident donation color=%d want green", got)
	}
	// Soft stamp with low confidence must not color.
	cloudflare := model.IngestedMessage{From: "Cloudflare <em@em1.cloudflare.com>", Subject: "Updates to managing AI crawlers on your account"}
	if ConfirmMatchedRule(mailRules, cloudflare, model.MessageFacts{MatchedRule: 1, RuleConfidence: 40, Title: "AI crawler controls"}) != 0 {
		t.Fatal("low confidence must not confirm")
	}
	if ColorForFact(mailRules, cloudflare, model.MessageFacts{MatchedRule: 1, RuleConfidence: 40, Title: "AI crawler controls"}) != 0 {
		t.Fatal("low-confidence cloudflare must not get finance color")
	}
	linkedin := model.IngestedMessage{From: "LinkedIn <jobs-noreply@linkedin.com>", Subject: "New jobs for you"}
	if ColorForFact(mailRules, linkedin, model.MessageFacts{MatchedRule: 1, RuleConfidence: 50}) != 0 {
		t.Fatal("matched_rule below confidence floor must not color")
	}
	if ColorFromMatchedRule(mailRules, 2) != NamedColors["blue"] {
		t.Fatal("index 2 should be job filter blue")
	}
	if ColorFromMatchedRule(mailRules, 9) != 0 {
		t.Fatal("out of range matched_rule should be ignored")
	}
	// Watch-claimed ignores matched_rule so job filter keywords win.
	claimed := ColorForFact(mailRules, model.IngestedMessage{
		From: "Greenhouse <noreply@greenhouse.io>", Subject: "Software Engineer early career role",
	}, model.MessageFacts{MatchedRule: 1, Claimed: true, RuleConfidence: 99, Title: "early career software engineer"})
	if claimed != NamedColors["blue"] {
		t.Fatalf("claimed watch should use keyword/job color=%d", claimed)
	}
}

func TestRuleHasEvidenceConfidenceGate(t *testing.T) {
	finance := model.MailRule{Type: model.RuleInstruction, Instruction: "keep emails about real personal financial transactions", Color: NamedColors["green"]}
	rules := []model.MailRule{finance}

	donation := model.IngestedMessage{
		From: "Tremendous <rewards@reward.tremendous.com>", Subject: "Thank you for your $5.00 USD donation",
		Body: "You donated $5.00 USD",
	}
	if ConfirmMatchedRule(rules, donation, model.MessageFacts{MatchedRule: 1, RuleConfidence: 85, Title: "Donation", Summary: "$5 donation"}) != 1 {
		t.Fatal("high-confidence donation should confirm transactions rule")
	}
	if ColorForFact(rules, donation, model.MessageFacts{MatchedRule: 1, RuleConfidence: 85}) != NamedColors["green"] {
		t.Fatal("donation should get finance color with high confidence")
	}
	if ConfirmMatchedRule(rules, donation, model.MessageFacts{MatchedRule: 1, RuleConfidence: 50}) != 0 {
		t.Fatal("below confidence floor must not confirm")
	}

	spotify := model.IngestedMessage{
		From: "Spotify <no-reply@alerts.spotify.com>", Subject: "353955 - Your Spotify login code",
		Body: "Your login code is 353955.",
	}
	spotifyFacts := model.MessageFacts{
		MatchedRule: 1, RuleConfidence: 95,
		Title: "Spotify login code", Outcome: "keep via qwen: payment confirmation",
	}
	if ConfirmMatchedRule(rules, spotify, spotifyFacts) != 0 {
		t.Fatal("OTP mail must not confirm non-auth keep rule even at high confidence")
	}
	if ColorForFact(rules, spotify, spotifyFacts) != 0 {
		t.Fatal("spotify login must not get finance color")
	}

	authRule := model.MailRule{Type: model.RuleInstruction, Instruction: "keep login verification codes", Color: NamedColors["blue"]}
	if ConfirmMatchedRule([]model.MailRule{authRule}, spotify, model.MessageFacts{MatchedRule: 1, RuleConfidence: 90, Title: "login code"}) != 1 {
		t.Fatal("auth rule may confirm OTP mail")
	}
}

func TestParseMailCommand(t *testing.T) {
	m := ParseMailCommand("mute community@extern.com")
	if m.Action != model.RuleMute || m.Pattern != "community@extern.com" {
		t.Fatalf("%+v", m)
	}
	dom := ParseMailCommand("mute @extern.com")
	if dom.Action != model.RuleMute || dom.Pattern != "@extern.com" {
		t.Fatalf("domain mute: %+v", dom)
	}
	dom2 := ParseMailCommand("mute extern.com")
	if dom2.Action != model.RuleMute || dom2.Pattern != "@extern.com" {
		t.Fatalf("bare domain mute: %+v", dom2)
	}
	bad := ParseMailCommand("do not show me factor emails again")
	if bad.Action != model.RuleMute || NormalizeMuteEmail(bad.Pattern) != "" || !strings.Contains(bad.Reply, "email") {
		t.Fatalf("brand mute should need email: %+v", bad)
	}
	u := ParseMailCommand("unmute community@extern.com")
	if u.Action != "unmute" || u.Pattern != "community@extern.com" {
		t.Fatalf("%+v", u)
	}
	a := ParseMailCommand("always show Osprey")
	if a.Action != model.RuleAlwaysShow || a.Pattern != "Osprey" {
		t.Fatalf("%+v", a)
	}
	j := ParseMailCommand("only show job search ones if they match ML or backend")
	if j.Action != model.RuleJobFilter || j.Pattern == "" {
		t.Fatalf("%+v", j)
	}
	j2 := ParseMailCommand("Set job alerts for full time roles within the United States targeted towards early career/new grads")
	if j2.Action != model.RuleJobFilter || !strings.Contains(strings.ToLower(j2.Pattern), "early career") {
		t.Fatalf("job alerts: %+v", j2)
	}
	if LooksLikeHelpRequest("Set job alerts for full time roles") {
		t.Fatal("job alerts is not help")
	}
	if ParseMailCommand("rules").Action != "list" {
		t.Fatal("rules")
	}
	if r := ParseMailCommand("remove 2"); r.Action != "remove_n" || r.Index != 2 {
		t.Fatalf("remove n: %+v", r)
	}
	if r := ParseMailCommand("forget the Extern rule"); r.Action != "remove" || !strings.EqualFold(r.Pattern, "Extern") {
		t.Fatalf("forget named: %+v", r)
	}
	if r := ParseMailCommand("stop watching Extern"); r.Action != "remove" || !strings.EqualFold(r.Pattern, "Extern") {
		t.Fatalf("stop watching: %+v", r)
	}
	if ParseMailCommand("clear rules").Action != "remove_all" {
		t.Fatal("clear rules")
	}
	if ParseMailCommand("help").Action != "help" {
		t.Fatal("help")
	}
	if c := ParseMailCommand("make it purple"); c.Action != "color" || !strings.EqualFold(c.ColorName, "purple") {
		t.Fatalf("make it: %+v", c)
	}
	if c := ParseMailCommand("make 2 blue"); c.Action != "color" || c.Index != 2 || !strings.EqualFold(c.ColorName, "blue") {
		t.Fatalf("make n: %+v", c)
	}
	if c := ParseMailCommand("hyundai is green"); c.Action != "color" || !strings.EqualFold(c.Pattern, "hyundai") {
		t.Fatalf("is color: %+v", c)
	}
	if ParseMailCommand("extern mail is important").Action == "color" {
		t.Fatal("important is not a color")
	}
	if c := ParseMailCommand("change rule 2 to blue"); c.Action != "color" || c.Index != 2 || !strings.EqualFold(c.ColorName, "blue") {
		t.Fatalf("change rule: %+v", c)
	}
}

func TestParseRuleEditsBatch(t *testing.T) {
	edits := ParseRuleEdits("remove 3,4,5 and change rule 2 to blue")
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
	if got := ParseIndexList("3, 4, and 5"); len(got) != 3 || got[0] != 3 || got[2] != 5 {
		t.Fatalf("%v", got)
	}
	if len(ParseRuleEdits("forget the Extern rule")) != 0 {
		t.Fatal("named remove is not a numbered edit")
	}
	if len(ParseRuleEdits("make it purple")) != 0 {
		t.Fatal("make it purple is a pronoun color, not a numbered edit")
	}
	typo := ParseRuleEdits("remove 3,4,5 and change ruke 2 to blue")
	if len(typo) != 2 {
		t.Fatalf("typo rule: %+v", typo)
	}
}

func TestMatchingRulesAndFormat(t *testing.T) {
	rules := []model.MailRule{
		{ID: "a", Type: model.RuleMute, Pattern: "news@g.factor75.com"},
		{ID: "b", Type: model.RuleInstruction, Pattern: "Treat Extern event updates as important", Instruction: "Treat Extern event updates as important"},
	}
	got := MatchingRules(rules, "extern")
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("extern match: %+v", got)
	}
	list := FormatRules(rules)
	if !strings.Contains(list, "1. mute news@g.factor75.com") || !strings.Contains(list, "2. Treat Extern") || !strings.Contains(list, "grey") {
		t.Fatalf("list: %q", list)
	}
	skipList := FormatRules([]model.MailRule{
		{Type: model.RuleMute, Pattern: "hireft"},
		{Type: model.RuleInstruction, Instruction: "Treat Extern event updates as important"},
		{Type: model.RuleInstruction, Instruction: "Treat as not important: job hunting product ads"},
	})
	if strings.Count(skipList, "grey") != 1 || strings.Contains(skipList, "product ads ·") {
		t.Fatalf("skip instructions should have no color: %q", skipList)
	}
	if !strings.Contains(skipList, "inactive") {
		t.Fatalf("legacy brand mute should be marked inactive: %q", skipList)
	}
	if strings.Contains(list, "remove 2") || strings.Contains(list, "forget extern") {
		t.Fatalf("list should not include how-to: %q", list)
	}
	if !strings.Contains(RemovedReply(got), "Extern") {
		t.Fatalf("removed reply: %q", RemovedReply(got))
	}
}

func TestDefaultColorHintAndHelp(t *testing.T) {
	got := WithDefaultColorHint("I'll treat Extern mail as important.")
	if !strings.Contains(got, "I'll treat Extern mail as important") || !strings.Contains(got, "default grey") || !strings.Contains(got, "make it purple") {
		t.Fatalf("%q", got)
	}
	skip := "I don't need email from extern, hireft, or any job hunting site where it's not a specific job alert but rather advertising the product"
	if RuleGetsColorHint(skip) || !LooksLikeSkipPreference(skip) {
		t.Fatal("block rules should not ask for a color")
	}
	if !RuleGetsColorHint("extern mail is important") {
		t.Fatal("keep rules still get a color")
	}
	if RuleShowsColor(model.MailRule{Type: model.RuleInstruction, Instruction: "Treat as not important: job hunting product ads"}) {
		t.Fatal("skip instruction should not show a color")
	}
	if !RuleShowsColor(model.MailRule{Type: model.RuleInstruction, Instruction: "Treat Extern event updates as important"}) {
		t.Fatal("keep instruction should show a color")
	}
	if !RuleShowsColor(model.MailRule{Type: model.RuleJobFilter, Pattern: "ML, backend", Color: NamedColors["blue"]}) {
		t.Fatal("job watch rules show a color because matching jobs appear in the digest")
	}
}

func TestSanitizeUserRuleParse(t *testing.T) {
	got := SanitizeUserRuleParse(model.UserRuleParse{
		Mutes:        []string{"extern", "hireft", "community@extern.com", "@extern.com", "job hunting site"},
		Instructions: []string{"treat this kind of mail as not important", "Skip job-site product ads; keep specific job alerts."},
	})
	if len(got.Mutes) != 2 {
		t.Fatalf("mutes=%v", got.Mutes)
	}
	seen := map[string]bool{}
	for _, m := range got.Mutes {
		seen[m] = true
	}
	if !seen["community@extern.com"] || !seen["@extern.com"] {
		t.Fatalf("mutes=%v", got.Mutes)
	}
	if len(got.Instructions) != 1 || !strings.Contains(got.Instructions[0], "job-site") {
		t.Fatalf("instructions=%v", got.Instructions)
	}
	hide := SanitizeUserRuleParse(model.UserRuleParse{
		Instructions: []string{"ignore new sign in emails"},
	})
	if len(hide.Mutes) != 0 || len(hide.Instructions) != 1 {
		t.Fatalf("category hide should stay instruction: mutes=%v ins=%v", hide.Mutes, hide.Instructions)
	}
	if UsableMutePattern("job hunting site") || UsableInstruction("treat this kind of mail as not important") {
		t.Fatal("vague mute/instruction should be rejected")
	}
	if !strings.Contains(RuleHelpFull(), "Hide one company") || !strings.Contains(RuleHelpFull(), "/mute") {
		t.Fatalf("help missing sender guidance: %q", RuleHelpFull())
	}
	if !strings.Contains(RuleHelpFull(), "Skip a kind of mail") || !strings.Contains(strings.ToLower(RuleHelpFull()), "skip / ignore") {
		t.Fatalf("help missing category guidance: %q", RuleHelpFull())
	}
	help := strings.ToLower(RuleHelpFull())
	if !strings.Contains(help, "be specific") || !strings.Contains(help, "confident") {
		t.Fatalf("help missing keep-rule specificity/confidence guidance: %q", RuleHelpFull())
	}
	if strings.Contains(RuleHelp(), "mute factor75") {
		t.Fatalf("short help should point at help: %q", RuleHelp())
	}
}

func TestMuteMatchesSenderEmailOnly(t *testing.T) {
	rules := []model.MailRule{{Type: model.RuleMute, Pattern: "community@extern.com"}}
	why, ok := FindMute(context.Background(), rules, model.IngestedMessage{
		From: "Carlinda @ Extern <community@extern.com>", Subject: "apply today", Body: "external link here",
	}, model.MessageFacts{})
	if !ok || !strings.Contains(why, "community@extern.com") {
		t.Fatalf("should mute exact From: %q ok=%v", why, ok)
	}
	if _, hit := FindMute(context.Background(), rules, model.IngestedMessage{
		From: "LinkedIn <jobs-noreply@linkedin.com>", Subject: "Junior role", Body: "jobs-0-external~eml apply",
	}, model.MessageFacts{}); hit {
		t.Fatal("body 'external' must not trip mute extern email")
	}
	if _, hit := FindMute(context.Background(), []model.MailRule{{Type: model.RuleMute, Pattern: "extern"}}, model.IngestedMessage{
		From: "Extern <community@extern.com>", Body: "hello from extern",
	}, model.MessageFacts{}); hit {
		t.Fatal("legacy brand mute must be inert")
	}

	domainRules := []model.MailRule{{Type: model.RuleMute, Pattern: "@extern.com"}}
	for _, from := range []string{
		"Eric <eric@extern.com>",
		"Daisy <daisy@extern.com>",
		"News <noreply@mail.extern.com>",
	} {
		if !MuteMatchesSender("@extern.com", from) {
			t.Fatalf("domain mute should hit %s", from)
		}
	}
	if MuteMatchesSender("@extern.com", "HireFT <alerts@hireft.com>") {
		t.Fatal("domain mute must not hit other domains")
	}
	if _, hit := FindMute(context.Background(), domainRules, model.IngestedMessage{
		From: "Eric <eric@extern.com>", Body: "hi",
	}, model.MessageFacts{}); !hit {
		t.Fatal("FindMute should honor @extern.com")
	}
	if NormalizeMuteTarget("extern.com") != "@extern.com" || NormalizeMuteTarget("mute @extern.com") != "@extern.com" {
		t.Fatalf("domain normalize: %q %q", NormalizeMuteTarget("extern.com"), NormalizeMuteTarget("mute @extern.com"))
	}
	if NormalizeMuteTarget("extern") != "" {
		t.Fatal("bare brand must still be rejected")
	}
}

func TestColorForMailUsesRule(t *testing.T) {
	mailRules := []model.MailRule{{Type: model.RuleInstruction, Pattern: "Treat Extern updates as important", Instruction: "Treat Extern updates as important", Color: NamedColors["purple"]}}
	// Instruction colors come from confirmed matched_rule + confidence, not keyword ColorForMail.
	got := ColorForFact(mailRules, model.IngestedMessage{From: "Extern <hi@extern.co>", Subject: "Wednesday event update"}, model.MessageFacts{
		MatchedRule: 1, RuleConfidence: 85, Title: "Extern event update",
	})
	if got != NamedColors["purple"] {
		t.Fatalf("color=%d", got)
	}
	if ColorForMail(mailRules, model.IngestedMessage{From: "Extern <hi@extern.co>", Subject: "Wednesday event update"}, model.MessageFacts{}) != 0 {
		t.Fatal("instruction rules must not color via ColorForMail keyword scan alone")
	}
	if ColorForMail(nil, model.IngestedMessage{Subject: "random"}, model.MessageFacts{}) != 0 {
		t.Fatal("no rule should stay unset so embed falls back to grey")
	}
}

func TestClassifyInboxRoutes(t *testing.T) {
	cases := []struct {
		in    string
		route model.InboxRoute
	}{
		{"", model.InboxEmpty},
		{"thanks", model.InboxAck},
		{"ok", model.InboxAck},
		{"got it.", model.InboxAck},
		{"rules", model.InboxCommand},
		{"help", model.InboxCommand},
		{"mute news@g.factor75.com", model.InboxCommand},
		{"mute @extern.com", model.InboxCommand},
		{"ignore new sign in emails", model.InboxInterpret},
		{"don't include tech news", model.InboxInterpret},
		{"skip job-site product ads", model.InboxInterpret},
		{"always show Osprey", model.InboxCommand},
		{"make it purple", model.InboxCommand},
		{"hyundai is green", model.InboxCommand},
		{"forget the Extern rule", model.InboxCommand},
		{"Set job alerts for full time roles within the United States targeted towards early career/new grads", model.InboxCommand},
		{"remove 3,4,5 and change rule 2 to blue", model.InboxEdits},
		{"remove 3, 4, and 5", model.InboxEdits},
		{"change rule 2 to blue", model.InboxEdits},
		{"did I get any hyundai emails this day?", model.InboxInsight},
		{"did I get any email from extern today?", model.InboxInsight},
		{"important emails from the past 24 hours", model.InboxInsight},
		{"what happened in my inbox?", model.InboxInsight},
		{"anything about the car?", model.InboxInsight},
		{"I don't need email from extern, hireft, or any job hunting site where it's not a specific job alert but rather advertising the product", model.InboxInterpret},
		{"extern mail is important", model.InboxInterpret},
	}
	for _, tc := range cases {
		got, _, _ := ClassifyInbox(tc.in)
		if got != tc.route {
			t.Fatalf("%q: got %s want %s", tc.in, got, tc.route)
		}
	}
	if LooksLikeJobPreference("I don't need a specific job alert, skip the ads") && !LooksLikeSkipPreference("I don't need a specific job alert, skip the ads") {
		t.Fatal("skip + job alert should not become a job filter")
	}
}

func TestLooksLikeInsight(t *testing.T) {
	if !LooksLikeInsight("did i get any hyundai emails this day?") {
		t.Fatal("hyundai question")
	}
	if LooksLikeInsight("extern mail is important") {
		t.Fatal("rule should not look like insight")
	}
	if !LooksLikeInsight("important emails from the past 24 hours") {
		t.Fatal("recap should look like insight")
	}
	pref := "I don't need email from extern, hireft, or any job hunting site where it's not a specific job alert but rather advertising the product"
	if LooksLikeInsight(pref) || !LooksLikeRulePreference(pref) {
		t.Fatal("skip-these-senders is a rule, not an inbox search")
	}
	if !LooksLikeInsight("did I get any email from extern today?") {
		t.Fatal("arrival question should still be insight")
	}
}

func TestApplyRulesMuteAndJobFilter(t *testing.T) {
	msgs := []model.IngestedMessage{
		{From: "Factor75 <news@g.factor75.com>", Subject: "Labor Day Sale"},
		{From: "Annie Collins <annie.collins@ripplematch.com>", Subject: "Ripplematch: New Match with BNP Paribas - Machine Learning Engineer"},
		{From: "LinkedIn <jobs-noreply@linkedin.com>", Subject: "Explore new jobs for Software engineer"},
		{From: "Osprey X <ospreyx13@gmail.com>", Subject: "Meeting time", Body: "Zoom at 5pm tomorrow"},
	}
	facts := []model.MessageFacts{
		{Kind: model.KindPromo, Who: "Factor75"},
		{Kind: model.KindNotice, Who: "Annie Collins", What: "New Match with BNP Paribas - Machine Learning Engineer"},
		{Kind: model.KindPromo, Who: "LinkedIn"},
		{Kind: model.KindNotice, Who: "Osprey X", What: "wants you to confirm a Zoom", When: "5pm tomorrow"},
	}
	mailRules := []model.MailRule{
		{Type: model.RuleMute, Pattern: "news@g.factor75.com"},
		{Type: model.RuleJobFilter, Pattern: "ML, backend"},
	}
	kept, noise := ApplyRules(context.Background(), msgs, facts, mailRules)
	if noise < 2 {
		t.Fatalf("expected muted/promo noise, kept=%+v noise=%d", kept, noise)
	}
	var text string
	for _, f := range kept {
		text += mail.CompileLine(f) + "\n"
	}
	if !model.ContainsAny(text, "Machine Learning", "BNP") {
		t.Fatalf("job filter dropped BNP ML: %q", text)
	}
	if model.ContainsAny(text, "Factor", "Labor Day") {
		t.Fatalf("factor leaked: %q", text)
	}
	if !model.ContainsAny(text, "Osprey") {
		t.Fatalf("meeting dropped: %q", text)
	}
}

func TestApplyRulesKeepsReplyToMeEvenIfMuted(t *testing.T) {
	msgs := []model.IngestedMessage{
		{From: "HireFT <alerts@hireft.com>", Subject: "Re: Your application", Body: "Can you hop on a call Thursday?", ReplyToMe: true},
	}
	facts := []model.MessageFacts{{Kind: model.KindPromo, Who: "HireFT"}}
	rules := []model.MailRule{{Type: model.RuleMute, Pattern: "alerts@hireft.com"}}
	kept, noise := ApplyRules(context.Background(), msgs, facts, rules)
	if noise != 0 || len(kept) != 1 {
		t.Fatalf("reply to owner should beat mute: kept=%+v noise=%d", kept, noise)
	}
	if !kept[0].ReplyToMe || kept[0].Kind != model.KindNotice {
		t.Fatalf("%+v", kept[0])
	}
}

func TestWatchRuleBeatsSkipWhenBothFit(t *testing.T) {
	rules := []model.MailRule{
		{Type: model.RuleJobFilter, Pattern: "full time roles within the United States targeted towards early career/new grads"},
		{Type: model.RuleInstruction, Instruction: "Skip job-hunting site emails that advertise the product. Keep specific job alerts."},
	}
	msg := model.IngestedMessage{
		From:    "Alerts <jobs@example.com>",
		Subject: "New grad software engineer — United States",
		Body:    "Full-time early career role. Apply to this specific opening.",
	}
	if !ClaimedByWatch(rules, msg, model.MessageFacts{}) {
		t.Fatal("watch filter should claim a matching posting")
	}
	kept, noise := ApplyRules(context.Background(),
		[]model.IngestedMessage{msg},
		[]model.MessageFacts{{Kind: model.KindPromo, Title: "", Summary: ""}},
		rules,
	)
	if noise != 0 || len(kept) != 1 || !kept[0].Claimed {
		t.Fatalf("skip should not veto a watched item Qwen marked promo: kept=%+v noise=%d", kept, noise)
	}
	ad := model.IngestedMessage{
		From:    "Alerts <jobs@example.com>",
		Subject: "Unlock premium job search tools",
		Body:    "Upgrade your subscription to see more postings.",
	}
	if ClaimedByWatch(rules, ad, model.MessageFacts{}) {
		t.Fatal("a product pitch should not be claimed just because a skip rule exists")
	}
	weak := model.IngestedMessage{From: "Deals <hi@list.com>", Subject: "See you next time — software tips"}
	if ClaimedByWatch(rules, weak, model.MessageFacts{}) {
		t.Fatal("a weak word like time should not claim promo")
	}
	if ClaimedByWatch([]model.MailRule{{
		Type: model.RuleInstruction, Instruction: "treat emails about finance update as important",
	}}, model.IngestedMessage{From: "Shop <a@b.com>", Subject: "February update"}, model.MessageFacts{}) {
		t.Fatal("a keep instruction should not auto-claim every update")
	}
}

func TestJobFilterColorAppliesToMatchingMail(t *testing.T) {
	mailRules := []model.MailRule{{Type: model.RuleJobFilter, Pattern: "early career, United States", Color: NamedColors["blue"]}}
	msg := model.IngestedMessage{From: "Jobs <j@x.com>", Subject: "Early career software engineer — United States"}
	if ColorForMail(mailRules, msg, model.MessageFacts{}) != NamedColors["blue"] {
		t.Fatalf("job filter color=%d", ColorForMail(mailRules, msg, model.MessageFacts{}))
	}
}

func TestHygieneFooterSkipsPeopleAndWantedMail(t *testing.T) {
	got := HygieneFooter([]senderFreq{
		{From: "Osprey X <ospreyx13@gmail.com>", Count: 4, PromoCount: 0},
		{From: "Empower <noreply@empower.com>", Count: 12, PromoCount: 0},
		{From: "Factor75 <news@g.factor75.com>", Count: 8, PromoCount: 8},
	}, nil)
	if model.ContainsAny(got, "Osprey") {
		t.Fatalf("personal sender in footer: %q", got)
	}
	if model.ContainsAny(got, "Empower") {
		t.Fatalf("wanted finance mail in footer: %q", got)
	}
	if !model.ContainsAny(got, "Factor") || !model.ContainsAny(got, "mute news@g.factor75.com") {
		t.Fatalf("expected mute suggestion for promo list: %q", got)
	}
	if model.ContainsAny(strings.ToLower(got), "unsubscribe") {
		t.Fatalf("should not push unsubscribe: %q", got)
	}
}

func TestHygieneFooterSkipsMuted(t *testing.T) {
	got := HygieneFooter([]senderFreq{
		{From: "Factor75 <news@g.factor75.com>", Count: 8, PromoCount: 8},
	}, []model.MailRule{{Type: model.RuleMute, Pattern: "news@g.factor75.com"}})
	if got != "" {
		t.Fatalf("muted promo still nudged: %q", got)
	}
}

