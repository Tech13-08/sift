package rules

import (
	"strings"

	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
)

// ConfirmMatchedRule keeps the model's matched_rule only when the mail corroborates that keep rule.
// Returns 0 when the index is missing or evidence is too weak (stops soft finance/job stamps).
func ConfirmMatchedRule(rules []model.MailRule, msg model.IngestedMessage, f model.MessageFacts) int {
	n := f.MatchedRule
	if n <= 0 {
		return 0
	}
	colorable := ColorableKeepRules(rules)
	if n > len(colorable) {
		return 0
	}
	if !RuleHasEvidence(colorable[n-1], msg, f) {
		return 0
	}
	return n
}

// RuleHasEvidence is true when the mail supports applying this keep/watch rule.
func RuleHasEvidence(r model.MailRule, msg model.IngestedMessage, f model.MessageFacts) bool {
	switch r.Type {
	case model.RuleJobFilter:
		tokens := jobFilterTokens([]model.MailRule{r})
		hay := strings.ToLower(msg.From + " " + msg.Subject + " " + f.Who + " " + f.What + " " + f.Title)
		return len(tokens) > 0 && mail.LooksJobish(hay) && matchesJobFilter(f, msg, tokens)
	case model.RuleAlwaysShow:
		return AlwaysShows([]model.MailRule{r}, msg)
	default:
		return instructionKeepEvidence(r, msg, f)
	}
}

func evidenceBlob(msg model.IngestedMessage, f model.MessageFacts) string {
	return strings.ToLower(strings.Join([]string{
		msg.From, msg.Subject, msg.Body, f.Who, f.What, f.Title, f.Summary, f.Outcome,
	}, " "))
}

func instructionKeepEvidence(r model.MailRule, msg model.IngestedMessage, f model.MessageFacts) bool {
	text := strings.ToLower(strings.TrimSpace(r.Instruction + " " + r.Pattern))
	if text == "" {
		return false
	}
	blob := evidenceBlob(msg, f)

	if ruleImpliesFinance(text) {
		return looksFinanceEvidence(blob)
	}
	if ruleImpliesApplication(text) {
		return looksApplicationEvidence(blob)
	}

	// Generic keep instruction: need a distinctive rule token in the mail (not filler like "update").
	hits := 0
	strong := false
	for _, tok := range ruleEvidenceTokens(text) {
		if !strings.Contains(blob, tok) {
			continue
		}
		hits++
		if len(tok) >= 6 && !weakEvidenceToken[tok] {
			strong = true
		}
	}
	return strong || hits >= 2
}

func ruleImpliesFinance(text string) bool {
	return model.ContainsAny(text, "finance", "financial", "payment", "money", "bank", "brokerage", "paycheck")
}

func ruleImpliesApplication(text string) bool {
	return model.ContainsAny(text, "application", "applied", "interview", "offer", "rejection") &&
		!model.ContainsAny(text, "product ad", "job-site", "job site")
}

func looksFinanceEvidence(blob string) bool {
	if mail.LooksMoneyEvent(blob) {
		return true
	}
	// Real money movement / confirmation — not "financial monitor" or generic account updates.
	if model.ContainsAny(blob,
		"donation", "donated", "payment confirmation", "payment received", "you paid", "paid $",
		"refund", "wire transfer", "trade confirmation", "dividend", "recurring payment",
		"charge of $", "charged $", "receipt for") {
		return true
	}
	if mail.MoneyAmount(blob) != "" && model.ContainsAny(blob, "payment", "paid", "donation", "donated", "refund", "charged", "receipt") {
		return true
	}
	return false
}

func looksApplicationEvidence(blob string) bool {
	if mail.LooksClosedApplication(blob) {
		return true
	}
	// Outcomes of an application the owner submitted — not "apply now" job blasts.
	if model.ContainsAny(blob, "apply now", "jobs for you", "explore new jobs", "new jobs matched", "still available", "you'd be a great fit") {
		return false
	}
	return model.ContainsAny(blob,
		"application was sent", "application sent", "your application to", "your application for",
		"applied to", "interview scheduled", "interview with you", "offer letter",
		"thanks for applying", "thank you for applying", "we received your application")
}

func ruleEvidenceTokens(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, tok := range strings.Fields(text) {
		tok = strings.Trim(tok, ".,;:\"'")
		if len(tok) < 4 || ruleMatchStop[tok] || weakEvidenceToken[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

var weakEvidenceToken = map[string]bool{
	"update": true, "updates": true, "about": true, "email": true, "emails": true,
	"mail": true, "mails": true, "important": true, "treat": true, "mention": true,
	"keep": true, "watch": true, "nuance": true, "names": true, "that": true,
	"this": true, "with": true, "from": true, "your": true, "their": true,
}
