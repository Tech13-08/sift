package mail

import (
	"context"
	"database/sql"
	"log"
	"regexp"
	"sift/summarizer-service/summarizer/model"
	"strings"
)

func ExtractFacts(msg model.IngestedMessage) (model.MessageFacts, bool) {
	from := strings.ToLower(msg.From)
	subject := strings.ToLower(msg.Subject)
	body := strings.ToLower(msg.Body)
	blob := from + " " + subject + " " + body
	who := NamedWho(msg, SenderWho(msg.From))
	when := timeFromText(msg.Subject + " " + msg.Body)

	if looksListBlast(blob) && !LooksClosedApplication(blob) && !LooksMoneyEvent(blob) && !LooksTimeAsk(blob) {
		return model.MessageFacts{Kind: model.KindPromo, Who: who}, true
	}
	if looksMarketingNoise(from, subject, blob) && !looksCostly(subject, body) {
		return model.MessageFacts{Kind: model.KindPromo, Who: who}, true
	}

	if what, ok := costlyWhat(msg, who, when); ok {
		f := model.MessageFacts{Kind: model.KindNotice, Who: who, What: what, When: when}
		f.Summary = CompileLine(f)
		return f, true
	}
	return model.MessageFacts{Kind: model.KindNotice, Who: who, When: when}, false
}

func costlyWhat(msg model.IngestedMessage, who, when string) (string, bool) {
	blob := strings.ToLower(msg.Subject + " " + msg.Body)
	if LooksClosedApplication(blob) {
		what := "rejected you"
		if role := detailAfterDash(msg.Subject); role != "" {
			what += " for " + role
		}
		return what, true
	}
	if LooksTimeAsk(blob) && when != "" {
		return "wants you to confirm " + timeAskNoun(blob), true
	}
	if LooksMoneyEvent(blob) {
		return "confirmed a payment", true
	}
	if looksAskedOfYou(blob) && (LooksHumanSender(msg.From) || when != "") {
		return "needs a reply", true
	}
	return "", false
}

func looksCostly(subject, body string) bool {
	blob := strings.ToLower(subject + " " + body)
	return LooksClosedApplication(blob) || LooksTimeAsk(blob) || LooksMoneyEvent(blob) || looksAskedOfYou(blob)
}

func CompileLine(f model.MessageFacts) string {
	if f.Kind == model.KindPromo {
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

func KeepWatchedMail(msg model.IngestedMessage, f model.MessageFacts) model.MessageFacts {
	f.Kind = model.KindNotice
	f.Claimed = true
	if strings.TrimSpace(f.Title) == "" {
		f.Title = model.CollapseSpace(msg.Subject)
	}
	if strings.TrimSpace(f.Summary) == "" {
		if line := CompileLine(f); line != "" {
			f.Summary = line
		} else {
			f.Summary = FallbackLine(msg)
		}
	}
	f.Title = DeFirstPerson(f.Title)
	f.Summary = DeFirstPerson(f.Summary)
	return f
}

func KeepReplyToMe(msg model.IngestedMessage, f model.MessageFacts) model.MessageFacts {
	f.Kind = model.KindNotice
	f.ReplyToMe = true
	if strings.TrimSpace(f.Title) == "" {
		f.Title = model.CollapseSpace(msg.Subject)
	}
	if strings.TrimSpace(f.Summary) == "" {
		if line := CompileLine(f); line != "" {
			f.Summary = line
		} else {
			f.Summary = FallbackLine(msg)
		}
	}
	f.Title = DeFirstPerson(f.Title)
	f.Summary = DeFirstPerson(f.Summary)
	return f
}

func ApplyBodyOutcome(msg model.IngestedMessage, f model.MessageFacts) model.MessageFacts {
	body := strings.ToLower(msg.Subject + " " + msg.Body)
	if LooksClosedApplication(body) {
		f.Kind = model.KindNotice
		if !model.ContainsAny(strings.ToLower(f.What), "reject", "not moving", "not selected") {
			what := "rejected you"
			if role := detailAfterDash(msg.Subject); role != "" {
				what += " for " + role
			}
			f.What = what
		}
	}
	if amt := MoneyAmount(msg.Body); amt != "" && LooksMoneyEvent(strings.ToLower(msg.Body+" "+msg.Subject)) {
		if !strings.Contains(f.What, "$") {
			if strings.TrimSpace(f.What) == "" {
				f.What = "confirmed a payment of " + amt
			} else {
				f.What = strings.TrimSpace(f.What) + " of " + amt
			}
		}
		f.Kind = model.KindNotice
	}
	if w := NamedWho(msg, f.Who); w != "" {
		f.Who = w
	}
	f.Summary = CompileLine(f)
	return f
}

var (
	thankYouFromPat = regexp.MustCompile(`(?i)\b(?:thank you|thanks)\s+from\s+(.+?)(?:\s*[-–—|]|\s+for\b|$)`)
	yourAccountPat  = regexp.MustCompile(`(?i)\byour\s+([A-Z][A-Za-z0-9&.,' ]{2,50}?)\s+account\b`)
	paymentWhoPat   = regexp.MustCompile(`(?i)^(.+?)\s+payment\b`)
)

func NamedWho(msg model.IngestedMessage, fallback string) string {
	if m := thankYouFromPat.FindStringSubmatch(msg.Subject); m != nil {
		if w := cleanWhoName(m[1]); w != "" {
			return w
		}
	}
	bestAcct := ""
	for _, m := range yourAccountPat.FindAllStringSubmatch(msg.Body, -1) {
		w := cleanWhoName(m[1])
		if w == "" || GenericAccountName(w) {
			continue
		}
		if bestAcct == "" || len(w) < len(bestAcct) {
			bestAcct = w
		}
	}
	if bestAcct != "" {
		return bestAcct
	}
	if m := paymentWhoPat.FindStringSubmatch(strings.TrimSpace(msg.Subject)); m != nil {
		if w := cleanWhoName(m[1]); w != "" {
			return w
		}
	}
	if fallback != "" {
		return fallback
	}
	return SenderWho(msg.From)
}

func cleanWhoName(s string) string {
	s = model.CollapseSpace(s)
	s = strings.Trim(s, ".,;:\"'")
	if s == "" || strings.Contains(s, "http") || len(s) > 60 {
		return ""
	}
	return s
}

func GenericAccountName(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return low == "finance" || low == "account" || model.ContainsAny(low,
		"email", "user", "online", "customer", "member", "bank", "card", "loan")
}

func PersistFacts(ctx context.Context, db *sql.DB, messages []model.IngestedMessage, facts []model.MessageFacts) {
	if db == nil {
		return
	}
	for i, msg := range messages {
		if msg.ID == "" || i >= len(facts) {
			continue
		}
		f := facts[i]
		if _, err := db.ExecContext(ctx, `
			UPDATE ingested_messages
			SET kind = $1, outcome = $2, fact_who = $3, fact_what = $4, fact_when = $5, fact_summary = $6
			WHERE id = $7::uuid
		`, model.NullIfEmpty(f.Kind), model.NullIfEmpty(f.Outcome), model.NullIfEmpty(f.Who),
			model.NullIfEmpty(f.What), model.NullIfEmpty(f.When), model.NullIfEmpty(f.Summary), msg.ID); err != nil {
			log.Printf("persist facts id=%s: %v", msg.ID, err)
		}
	}
}

func SenderWho(from string) string {
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
	for _, sep := range []string{" - ", " — ", " – ", " | ", " || "} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			tail := strings.TrimSpace(s[i+len(sep):])
			if tail != "" && !strings.EqualFold(tail, "thank you") {
				return model.CollapseSpace(tail)
			}
		}
	}
	low := strings.ToLower(s)
	if i := strings.Index(low, "new match with "); i >= 0 {
		return model.CollapseSpace(s[i+len("new match with "):])
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

func looksListBlast(blob string) bool {
	return model.ContainsAny(blob,
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
	return model.ContainsAny(blob, "% off", "ending soon", "prices increase", "explore new jobs", "jobs for you", "hackathons just for you")
}

func LooksClosedApplication(blob string) bool {
	return model.ContainsAny(blob,
		"will not be moving", "not moving forward", "not to move you forward", "decided not to move",
		"not be proceeding", "will not be proceeding",
		"other candidates", "other applicants", "we regret to inform", "unfortunately we",
		"not selected", "no longer being considered", "position has been filled",
		"decided to move forward with other", "chosen to pursue other",
		"declined your", "rejected you", "rejected your")
}

var amountPat = regexp.MustCompile(`\$[\d,]+(?:\.\d{2})?`)

func MoneyAmount(s string) string {
	return amountPat.FindString(s)
}

func looksSoon(when string) bool {
	w := strings.ToLower(when)
	return model.ContainsAny(w, "tomorrow", "today", "tonight", "this evening", "in an hour")
}

func LooksTimeAsk(blob string) bool {
	return model.ContainsAny(blob, "zoom", "meet.google", "teams.microsoft", "appointment", "campus tour", "rsvp", "meeting time", "confirm the meeting", "call at ")
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

func LooksMoneyEvent(blob string) bool {
	return model.ContainsAny(blob, "payment confirmation", "recurring payment", "payment received")
}

func looksAskedOfYou(blob string) bool {
	return model.ContainsAny(blob, "can you", "could you", "please confirm", "please reply", "let me know if", "need you to")
}

func LooksHumanSender(from string) bool {
	if IsPersonalSender(from) {
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
	return !model.ContainsAny(low, "noreply", "no-reply", "donotreply", "news@", "marketing@")
}

func LooksJobish(s string) bool {
	return model.ContainsAny(strings.ToLower(s),
		"engineer", "hiring", "internship", "new match", "software", "recruiter", "interview",
		"job alert", "new grad", "early career", "open role", "job opening", "now hiring")
}

func FallbackLine(msg model.IngestedMessage) string {
	from := strings.TrimSpace(msg.From)
	subject := strings.TrimSpace(msg.Subject)
	if from == "" {
		from = "(unknown sender)"
	}
	if subject == "" {
		subject = "(no subject)"
	}
	return from + " - " + subject
}
