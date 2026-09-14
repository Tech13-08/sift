package main

import (
	"context"
	"database/sql"
	"log"
	"regexp"
	"strings"
)

const (
	kindPromo  = "promo"
	kindNotice = "notice"
)

type messageFacts struct {
	Kind      string
	Outcome   string
	Who       string
	What      string
	When      string
	Summary   string
	From      string
	Title     string
	Mailbox   string
	Color     int
	ReplyToMe bool
	Claimed   bool
}

func extractFacts(msg ingestedMessage) (messageFacts, bool) {
	from := strings.ToLower(msg.from)
	subject := strings.ToLower(msg.subject)
	body := strings.ToLower(msg.body)
	blob := from + " " + subject + " " + body
	who := namedWho(msg, senderWho(msg.from))
	when := timeFromText(msg.subject + " " + msg.body)

	if looksListBlast(blob) && !looksClosedApplication(blob) && !looksMoneyEvent(blob) && !looksTimeAsk(blob) {
		return messageFacts{Kind: kindPromo, Who: who}, true
	}
	if looksMarketingNoise(from, subject, blob) && !looksCostly(subject, body) {
		return messageFacts{Kind: kindPromo, Who: who}, true
	}

	if what, ok := costlyWhat(msg, who, when); ok {
		f := messageFacts{Kind: kindNotice, Who: who, What: what, When: when}
		f.Summary = compileLine(f)
		return f, true
	}
	return messageFacts{Kind: kindNotice, Who: who, When: when}, false
}

func costlyWhat(msg ingestedMessage, who, when string) (string, bool) {
	blob := strings.ToLower(msg.subject + " " + msg.body)
	if looksClosedApplication(blob) {
		what := "rejected you"
		if role := detailAfterDash(msg.subject); role != "" {
			what += " for " + role
		}
		return what, true
	}
	if looksTimeAsk(blob) && when != "" {
		return "wants you to confirm " + timeAskNoun(blob), true
	}
	if looksMoneyEvent(blob) {
		return "confirmed a payment", true
	}
	if looksAskedOfYou(blob) && (looksHumanSender(msg.from) || when != "") {
		return "needs a reply", true
	}
	return "", false
}

func looksCostly(subject, body string) bool {
	blob := strings.ToLower(subject + " " + body)
	return looksClosedApplication(blob) || looksTimeAsk(blob) || looksMoneyEvent(blob) || looksAskedOfYou(blob)
}

func compileLine(f messageFacts) string {
	if f.Kind == kindPromo {
		return ""
	}
	who := strings.TrimSpace(f.Who)
	what := strings.TrimSpace(f.What)
	when := strings.TrimSpace(f.When)
	if what == "" {
		return ""
	}
	line := what
	if who != "" && !strings.HasPrefix(strings.ToLower(what), strings.ToLower(who)) {
		line = who + " " + what
	}
	if when != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(when)) {
		line += " " + when
	}
	if !strings.HasSuffix(line, ".") {
		line += "."
	}
	return line
}

func extractAllFacts(ctx context.Context, messages []ingestedMessage, rules []mailRule) []messageFacts {
	sys := categorizeOnePrompt
	if appendix := rulePromptAppendix(rules); appendix != "" {
		sys += "\n\nUser rules:\n" + appendix
	}
	out := make([]messageFacts, len(messages))
	for i, msg := range messages {
		if muted(rules, msg) && !msg.replyToMe {
			why := "muted sender"
			logDecide("skip", "mute", msg.from, msg.subject, why)
			out[i] = messageFacts{Kind: kindPromo, From: msg.from, Mailbox: msg.mailbox, Outcome: decisionOutcome("skip", "mute", why)}
			continue
		}
		claimed := claimedByWatch(rules, msg, messageFacts{})
		f, err := categorizeOneEmail(ctx, msg, sys, claimed)
		if err != nil {
			log.Printf("categorize fallback: %v", err)
			f, _ = extractFacts(msg)
			if f.Kind == kindNotice && f.Title == "" {
				f.Title = collapseSpace(msg.subject)
			}
			if f.Kind == kindNotice && f.Summary == "" {
				f.Summary = compileLine(f)
			}
			f.Title = deFirstPerson(f.Title)
			f.Summary = deFirstPerson(f.Summary)
			if f.Outcome == "" {
				if f.Kind == kindPromo {
					f.Outcome = decisionOutcome("skip", "heuristic", "fallback without qwen")
				} else {
					f.Outcome = decisionOutcome("keep", "heuristic", "fallback without qwen")
				}
			}
		}
		if f.Who == "" {
			f.Who = namedWho(msg, senderWho(msg.from))
		}
		action := "skip"
		if f.Kind == kindNotice {
			action = "keep"
		}
		logDecide(action, "qwen", msg.from, msg.subject, f.Outcome)
		if msg.replyToMe {
			f = keepReplyToMe(msg, f)
			f.Outcome = decisionOutcome("keep", "reply", "thread you already wrote in")
			logDecide("keep", "reply", msg.from, msg.subject, "thread you already wrote in")
		}
		if claimedByWatch(rules, msg, f) {
			before := f.Kind
			f = keepWatchedMail(msg, f)
			f.Outcome = decisionOutcome("keep", "watch", "matches a watch/keep rule")
			if before != kindNotice {
				logDecide("keep", "watch", msg.from, msg.subject, "watch rule overrode skip")
			} else {
				logDecide("keep", "watch", msg.from, msg.subject, "matches a watch/keep rule")
			}
		}
		out[i] = f
		out[i].From = msg.from
		out[i].Mailbox = msg.mailbox
	}
	return out
}

func keepWatchedMail(msg ingestedMessage, f messageFacts) messageFacts {
	f.Kind = kindNotice
	f.Claimed = true
	if strings.TrimSpace(f.Title) == "" {
		f.Title = collapseSpace(msg.subject)
	}
	if strings.TrimSpace(f.Summary) == "" {
		if line := compileLine(f); line != "" {
			f.Summary = line
		} else {
			f.Summary = fallbackLine(msg)
		}
	}
	f.Title = deFirstPerson(f.Title)
	f.Summary = deFirstPerson(f.Summary)
	return f
}

func keepReplyToMe(msg ingestedMessage, f messageFacts) messageFacts {
	f.Kind = kindNotice
	f.ReplyToMe = true
	if strings.TrimSpace(f.Title) == "" {
		f.Title = collapseSpace(msg.subject)
	}
	if strings.TrimSpace(f.Summary) == "" {
		if line := compileLine(f); line != "" {
			f.Summary = line
		} else {
			f.Summary = fallbackLine(msg)
		}
	}
	f.Title = deFirstPerson(f.Title)
	f.Summary = deFirstPerson(f.Summary)
	return f
}

func applyBodyOutcome(msg ingestedMessage, f messageFacts) messageFacts {
	body := strings.ToLower(msg.subject + " " + msg.body)
	if looksClosedApplication(body) {
		f.Kind = kindNotice
		if !containsAny(strings.ToLower(f.What), "reject", "not moving", "not selected") {
			what := "rejected you"
			if role := detailAfterDash(msg.subject); role != "" {
				what += " for " + role
			}
			f.What = what
		}
	}
	if amt := moneyAmount(msg.body); amt != "" && looksMoneyEvent(strings.ToLower(msg.body+" "+msg.subject)) {
		if !strings.Contains(f.What, "$") {
			if strings.TrimSpace(f.What) == "" {
				f.What = "confirmed a payment of " + amt
			} else {
				f.What = strings.TrimSpace(f.What) + " of " + amt
			}
		}
		f.Kind = kindNotice
	}
	if w := namedWho(msg, f.Who); w != "" {
		f.Who = w
	}
	f.Summary = compileLine(f)
	return f
}

var (
	thankYouFromPat = regexp.MustCompile(`(?i)\b(?:thank you|thanks)\s+from\s+(.+?)(?:\s*[-–—|]|\s+for\b|$)`)
	yourAccountPat  = regexp.MustCompile(`(?i)\byour\s+([A-Z][A-Za-z0-9&.,' ]{2,50}?)\s+account\b`)
	paymentWhoPat   = regexp.MustCompile(`(?i)^(.+?)\s+payment\b`)
)

func namedWho(msg ingestedMessage, fallback string) string {
	if m := thankYouFromPat.FindStringSubmatch(msg.subject); m != nil {
		if w := cleanWhoName(m[1]); w != "" {
			return w
		}
	}
	bestAcct := ""
	for _, m := range yourAccountPat.FindAllStringSubmatch(msg.body, -1) {
		w := cleanWhoName(m[1])
		if w == "" || genericAccountName(w) {
			continue
		}
		if bestAcct == "" || len(w) < len(bestAcct) {
			bestAcct = w
		}
	}
	if bestAcct != "" {
		return bestAcct
	}
	if m := paymentWhoPat.FindStringSubmatch(strings.TrimSpace(msg.subject)); m != nil {
		if w := cleanWhoName(m[1]); w != "" {
			return w
		}
	}
	if fallback != "" {
		return fallback
	}
	return senderWho(msg.from)
}

func cleanWhoName(s string) string {
	s = collapseSpace(s)
	s = strings.Trim(s, ".,;:\"'")
	if s == "" || strings.Contains(s, "http") || len(s) > 60 {
		return ""
	}
	return s
}

func genericAccountName(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return low == "finance" || low == "account" || containsAny(low,
		"email", "user", "online", "customer", "member", "bank", "card", "loan")
}

func persistFacts(ctx context.Context, db *sql.DB, messages []ingestedMessage, facts []messageFacts) {
	if db == nil {
		return
	}
	for i, msg := range messages {
		if msg.id == "" || i >= len(facts) {
			continue
		}
		f := facts[i]
		if _, err := db.ExecContext(ctx, `
			UPDATE ingested_messages
			SET kind = $1, outcome = $2, fact_who = $3, fact_what = $4, fact_when = $5, fact_summary = $6
			WHERE id = $7::uuid
		`, nullIfEmpty(f.Kind), nullIfEmpty(f.Outcome), nullIfEmpty(f.Who),
			nullIfEmpty(f.What), nullIfEmpty(f.When), nullIfEmpty(f.Summary), msg.id); err != nil {
			log.Printf("persist facts id=%s: %v", msg.id, err)
		}
	}
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func senderWho(from string) string {
	from = strings.TrimSpace(from)
	if from == "" {
		return ""
	}
	if i := strings.Index(from, "<"); i > 0 {
		name := strings.Trim(strings.TrimSpace(from[:i]), `"'`)
		if name != "" {
			return name
		}
		from = from[i:]
	}
	addr := from
	if i := strings.Index(from, "<"); i >= 0 {
		end := strings.Index(from, ">")
		if end > i {
			addr = from[i+1 : end]
		}
	}
	local, _, _ := strings.Cut(addr, "@")
	if local == "" || local == "noreply" || local == "no-reply" || local == "donotreply" || local == "updates" {
		if _, domain, ok := strings.Cut(addr, "@"); ok {
			return brandFromDomain(domain)
		}
	}
	return strings.TrimSpace(local)
}

func brandFromDomain(domain string) string {
	domain = strings.ToLower(domain)
	for _, p := range []string{"email.", "mail.", "e.", "g."} {
		domain = strings.TrimPrefix(domain, p)
	}
	host, _, _ := strings.Cut(domain, ".")
	if host == "" {
		return domain
	}
	return strings.ToUpper(host[:1]) + host[1:]
}

func detailAfterDash(subject string) string {
	s := strings.TrimSpace(subject)
	if i := strings.Index(s, ": "); i > 0 && i < 24 {
		s = strings.TrimSpace(s[i+2:])
	}
	for _, sep := range []string{" — ", " – ", " - ", " | ", " || "} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			tail := strings.TrimSpace(s[i+len(sep):])
			if tail != "" && !strings.EqualFold(tail, "thank you") {
				return collapseSpace(tail)
			}
		}
	}
	low := strings.ToLower(s)
	if i := strings.Index(low, "new match with "); i >= 0 {
		return collapseSpace(s[i+len("new match with "):])
	}
	return ""
}

var timePat = regexp.MustCompile(`(?i)\b(?:at\s+)?(\d{1,2}(?::\d{2})?\s*(?:am|pm))\b(?:\s+(tomorrow|today|tonight))?`)

func timeFromText(s string) string {
	m := timePat.FindStringSubmatch(s)
	if m == nil {
		if strings.Contains(strings.ToLower(s), "tomorrow") {
			return "tomorrow"
		}
		return ""
	}
	when := strings.TrimSpace(m[1])
	if len(m) > 2 && strings.TrimSpace(m[2]) != "" {
		when += " " + strings.ToLower(strings.TrimSpace(m[2]))
	} else if strings.Contains(strings.ToLower(s), "tomorrow") {
		when += " tomorrow"
	}
	return when
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func looksListBlast(blob string) bool {
	return containsAny(blob,
		"explore their open roles", "open roles and apply", "candidates this week",
		"these companies sent", "sent assessments to", "apply while they")
}

func looksMarketingNoise(from, subject, blob string) bool {
	if strings.Contains(from, "jobs-noreply") || strings.Contains(from, "groups-noreply") {
		return true
	}
	if strings.Contains(subject, "went live") || strings.Contains(subject, "just went live") {
		return true
	}
	return containsAny(blob, "% off", "ending soon", "prices increase", "explore new jobs", "jobs for you", "hackathons just for you")
}

func looksClosedApplication(blob string) bool {
	return containsAny(blob,
		"will not be moving", "not moving forward", "not to move you forward", "decided not to move",
		"not be proceeding", "will not be proceeding",
		"other candidates", "other applicants", "we regret to inform", "unfortunately we",
		"not selected", "no longer being considered", "position has been filled",
		"decided to move forward with other", "chosen to pursue other",
		"declined your", "rejected you", "rejected your")
}

var amountPat = regexp.MustCompile(`\$[\d,]+(?:\.\d{2})?`)

func moneyAmount(s string) string {
	return amountPat.FindString(s)
}

func looksSoon(when string) bool {
	w := strings.ToLower(when)
	return containsAny(w, "tomorrow", "today", "tonight", "this evening", "in an hour")
}

func looksTimeAsk(blob string) bool {
	return containsAny(blob, "zoom", "meet.google", "teams.microsoft", "appointment", "campus tour", "rsvp", "meeting time", "confirm the meeting", "call at ")
}

func timeAskNoun(blob string) string {
	switch {
	case strings.Contains(blob, "zoom"):
		return "a Zoom"
	case strings.Contains(blob, "tour"):
		return "a tour"
	case strings.Contains(blob, "appointment"):
		return "an appointment"
	default:
		return "a time"
	}
}

func looksMoneyEvent(blob string) bool {
	return containsAny(blob, "payment confirmation", "recurring payment", "payment received")
}

func looksAskedOfYou(blob string) bool {
	return containsAny(blob, "can you", "could you", "please confirm", "please reply", "let me know if", "need you to")
}

func looksHumanSender(from string) bool {
	if isPersonalSender(from) {
		return true
	}
	name := ""
	if i := strings.Index(from, "<"); i > 0 {
		name = strings.Trim(strings.TrimSpace(from[:i]), `"'`)
	}
	if name == "" || !strings.Contains(name, " ") {
		return false
	}
	low := strings.ToLower(from)
	return !containsAny(low, "noreply", "no-reply", "donotreply", "news@", "marketing@")
}

func looksJobish(s string) bool {
	return containsAny(strings.ToLower(s),
		"engineer", "hiring", "internship", "new match", "software", "recruiter", "interview",
		"job alert", "new grad", "early career", "open role", "job opening", "now hiring")
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
