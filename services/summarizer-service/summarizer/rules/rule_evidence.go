package rules

import (
	"strings"

	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
)

// Minimum model confidence (0-100) to accept matched_rule for color / confirmation.
const RuleConfidenceMin = 70

// ConfirmMatchedRule keeps the model's matched_rule only when confidence is high enough
// and the pick is not an obvious OTP↔unrelated-rule mismatch.
func ConfirmMatchedRule(rules []model.MailRule, msg model.IngestedMessage, f model.MessageFacts) int {
	n := f.MatchedRule
	if n <= 0 || f.RuleConfidence < RuleConfidenceMin {
		return 0
	}
	colorable := ColorableKeepRules(rules)
	if n > len(colorable) {
		return 0
	}
	r := colorable[n-1]
	content := mailContentBlob(msg, f)
	ruleText := strings.ToLower(strings.TrimSpace(r.Instruction + " " + r.Pattern))
	// Thin generic guard: login/OTP mail must not confirm non-auth keep rules.
	if looksAuthOrLoginCode(content) && !ruleImpliesAuth(ruleText) {
		return 0
	}
	return n
}

// RuleHasEvidence is for watch/job filters and always-show matching.
// Instruction keep colors use ConfirmMatchedRule (matched_rule + rule_confidence).
func RuleHasEvidence(r model.MailRule, msg model.IngestedMessage, f model.MessageFacts) bool {
	switch r.Type {
	case model.RuleJobFilter:
		tokens := jobFilterTokens([]model.MailRule{r})
		hay := strings.ToLower(msg.From + " " + msg.Subject + " " + f.Who + " " + f.What + " " + f.Title)
		return len(tokens) > 0 && mail.LooksJobish(hay) && matchesJobFilter(f, msg, tokens)
	case model.RuleAlwaysShow:
		return AlwaysShows([]model.MailRule{r}, msg)
	default:
		return false
	}
}

func mailContentBlob(msg model.IngestedMessage, f model.MessageFacts) string {
	return strings.ToLower(strings.Join([]string{
		msg.From, msg.Subject, msg.Body, f.Who, f.What, f.Title, f.Summary,
	}, " "))
}

func looksAuthOrLoginCode(blob string) bool {
	return model.ContainsAny(blob,
		"login code", "sign-in code", "signin code", "sign in code",
		"verification code", "security code", "one-time code", "one time code",
		"otp", "2fa", "two-factor", "two factor", "authentication code", "auth code",
		"passcode", "magic link")
}

func ruleImpliesAuth(text string) bool {
	return model.ContainsAny(text,
		"login", "sign-in", "signin", "sign in", "verification", "verify", "otp",
		"2fa", "two-factor", "two factor", "passcode", "magic link", "security code")
}
