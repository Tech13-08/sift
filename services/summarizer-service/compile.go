package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

type digestPayload struct {
	Content string
	Embeds  []discordEmbed
	Summary string
}

func buildDigest(ctx context.Context, db *sql.DB, u digestUser, messages []ingestedMessage) (digestPayload, error) {
	if len(messages) == 0 {
		return digestPayload{}, nil
	}
	loc := locationOrUTC(u.Timezone)
	now := time.Now()
	rules, err := loadRules(ctx, db, u.ID)
	if err != nil {
		log.Printf("load rules: %v", err)
	}
	facts := extractAllFacts(ctx, messages, rules)
	persistFacts(ctx, db, messages, facts)
	for i, f := range facts {
		log.Printf("fact id=%s kind=%s title=%q outcome=%q", messages[i].id, f.Kind, f.Title, f.Outcome)
	}
	kept, noise := applyRules(messages, facts, rules)
	log.Printf("rules kept=%d noise=%d", len(kept), noise)
	kept = cullUnimportant(ctx, kept)
	kept = collapseRelatedFacts(kept)
	kept = organizeByMailbox(kept)

	header := siftedHeader(now, loc)
	greeting := ""
	if drafted, err := draftGreeting(ctx, now.In(loc)); err != nil {
		log.Printf("greeting: %v", err)
		greeting = fallbackGreeting(now.In(loc))
	} else {
		greeting = drafted
	}

	embeds := embedsFromFacts(kept)
	log.Printf("digest embeds=%d titles=%q", len(embeds), embedTitles(embeds))
	content := header
	if greeting != "" {
		content += "\n\n" + greeting
	}
	if len(embeds) == 0 {
		content += "\n\nNothing important today."
	} else if len(embeds) > discordEmbedsPerMessage {
		content += fmt.Sprintf("\n\n%d items · page 1/%d", len(embeds), (len(embeds)+discordEmbedsPerMessage-1)/discordEmbedsPerMessage)
	}
	return digestPayload{
		Content: content,
		Embeds:  embeds,
		Summary: digestTextSummary(content, embeds),
	}, nil
}

// organizeByMailbox clusters kept items by registered inbox when a day spans more than one.
// Single-inbox days stay unchanged (no mailbox stamp on titles).
func organizeByMailbox(kept []messageFacts) []messageFacts {
	if len(kept) <= 1 {
		return kept
	}
	mailboxes := digestMailboxes(kept)
	if len(mailboxes) <= 1 {
		return kept
	}
	var out []messageFacts
	for _, mb := range mailboxes {
		for _, f := range kept {
			if f.Mailbox == mb {
				out = append(out, stampMailboxTitle(f, mb))
			}
		}
	}
	return out
}

func digestMailboxes(kept []messageFacts) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range kept {
		mb := strings.TrimSpace(f.Mailbox)
		if seen[mb] {
			continue
		}
		seen[mb] = true
		out = append(out, mb)
	}
	return out
}

func stampMailboxTitle(f messageFacts, mailbox string) messageFacts {
	label := strings.TrimSpace(mailbox)
	if label == "" {
		return f
	}
	f.Mailbox = mailbox
	title := strings.TrimSpace(f.Title)
	if title == "" || strings.Contains(strings.ToLower(title), strings.ToLower(label)) {
		return f
	}
	f.Title = title + " · " + label
	return f
}

func collapseRelatedFacts(kept []messageFacts) []messageFacts {
	if len(kept) <= 1 {
		return kept
	}
	var out []messageFacts
	merged := map[int]bool{}
	for i, a := range kept {
		if merged[i] {
			continue
		}
		cur := a
		if isPaymentFact(a) {
			for j := i + 1; j < len(kept); j++ {
				if merged[j] {
					continue
				}
				if samePaymentEvent(a, kept[j]) {
					cur = mergePaymentFacts(cur, kept[j])
					merged[j] = true
				}
			}
		}
		out = append(out, cur)
	}
	if len(out) < len(kept) {
		log.Printf("collapsed %d facts into %d", len(kept), len(out))
	}
	return out
}

func isPaymentFact(f messageFacts) bool {
	blob := strings.ToLower(f.Title + " " + f.Summary + " " + f.What)
	return looksMoneyEvent(blob) || containsAny(blob, "payment", "paid your", "bill")
}

func samePaymentEvent(a, b messageFacts) bool {
	if !isPaymentFact(a) || !isPaymentFact(b) {
		return false
	}
	ka, kb := paymentWhoKey(a), paymentWhoKey(b)
	if ka != "" && ka == kb {
		return true
	}
	return shareBrandToken(a, b)
}

func paymentWhoKey(f messageFacts) string {
	who := strings.ToLower(collapseSpace(f.Who))
	if who == "" || genericAccountName(who) {
		return ""
	}
	return who
}

func shareBrandToken(a, b messageFacts) bool {
	left := brandTokens(a.Who + " " + a.Title)
	right := brandTokens(b.Who + " " + b.Title)
	for tok := range left {
		if right[tok] {
			return true
		}
	}
	return false
}

func brandTokens(s string) map[string]bool {
	stop := map[string]bool{
		"payment": true, "confirmation": true, "received": true, "account": true,
		"recurring": true, "your": true, "the": true, "for": true, "and": true,
		"from": true, "this": true, "that": true, "with": true, "paid": true,
		"bill": true, "amount": true, "thank": true, "thanks": true,
	}
	out := map[string]bool{}
	for _, part := range strings.Fields(strings.ToLower(s)) {
		part = strings.Trim(part, ".,;:\"'`")
		if len(part) < 5 || stop[part] || genericAccountName(part) {
			continue
		}
		out[part] = true
	}
	return out
}

func mergePaymentFacts(a, b messageFacts) messageFacts {
	out := b
	if !usableWho(out.Who) {
		out.Who = a.Who
	}
	out.Title = preferredPaymentTitle(a, b)
	out.Summary = clumpPaymentSummary(a, b)
	if out.Color == 0 {
		out.Color = a.Color
	}
	return out
}

func usableWho(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || genericAccountName(s) {
		return false
	}
	low := strings.ToLower(s)
	return !strings.Contains(low, "please") && !strings.Contains(s, ",")
}

func preferredPaymentTitle(a, b messageFacts) string {
	score := func(f messageFacts) int {
		t := strings.ToLower(f.Title)
		n := 0
		if strings.Contains(t, "received") {
			n += 2
		}
		if strings.Contains(t, "20") {
			n++
		}
		if strings.Contains(t, "account") {
			n--
		}
		return n
	}
	if strings.TrimSpace(a.Title) != "" && score(a) > score(b) {
		return a.Title
	}
	return firstNonEmpty(b.Title, a.Title)
}

func preferredWho(a, b messageFacts) string {
	if usableWho(b.Who) {
		return strings.TrimSpace(b.Who)
	}
	if usableWho(a.Who) {
		return strings.TrimSpace(a.Who)
	}
	return firstNonEmpty(strings.TrimSpace(b.Who), strings.TrimSpace(a.Who))
}

func clumpPaymentSummary(a, b messageFacts) string {
	blob := a.Summary + " " + b.Summary + " " + a.Title + " " + b.Title
	who := preferredWho(a, b)
	amt := moneyAmount(blob)
	forWhat := paymentForWhat(a, b, who)
	received := containsAny(strings.ToLower(blob), "received", "receipt")
	verb := "confirmed"
	if received {
		verb = "received"
	}

	var s string
	switch {
	case who != "" && amt != "" && received:
		s = who + " received your " + amt + " payment"
	case who != "" && amt != "":
		s = who + " confirmed your " + amt + " payment"
	case who != "" && received:
		s = who + " received your payment"
	case who != "":
		s = who + " confirmed your payment"
	case amt != "":
		s = "Your " + amt + " payment was " + verb
	default:
		s = clumpSentences(a.Summary, b.Summary)
	}
	if forWhat != "" && !containsFold(s, forWhat) {
		s += " for " + forWhat
	}
	s = strings.TrimSpace(s)
	if s != "" && !strings.HasSuffix(s, ".") {
		s += "."
	}
	if settle := settlementHint(blob); settle != "" && !containsFold(s, settle) {
		s += " It may take " + settle + " to show in your bank account."
	}
	return strings.TrimSpace(s)
}

var (
	forWhatPat     = regexp.MustCompile(`(?i)\bfor (?:the |your |a )?(.+?)(?:\.|,|;|$)`)
	settlementPat  = regexp.MustCompile(`(?i)(\d+\s*[-–]\s*\d+\s+business days)`)
	paymentForSkip = []string{"account", "website", "information", "review"}
)

func paymentForWhat(a, b messageFacts, who string) string {
	var best string
	for _, src := range []string{a.Title, b.Title, a.Summary, b.Summary} {
		for _, m := range forWhatPat.FindAllStringSubmatch(src, -1) {
			w := collapseSpace(strings.Trim(m[1], " .,"))
			if w == "" || len(w) > 60 {
				continue
			}
			low := strings.ToLower(w)
			if who != "" && strings.Contains(low, strings.ToLower(who)) {
				continue
			}
			if containsAny(low, paymentForSkip...) || strings.Contains(w, "$") || strings.Contains(low, "amount") {
				continue
			}
			if best == "" || len(w) < len(best) {
				best = w
			}
		}
	}
	return best
}

func settlementHint(blob string) string {
	m := settlementPat.FindStringSubmatch(blob)
	if m == nil {
		return ""
	}
	return collapseSpace(m[1])
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func clumpSentences(parts ...string) string {
	var kept []string
	for _, part := range parts {
		for _, raw := range splitSentences(part) {
			s := strings.TrimSpace(raw)
			if s == "" || looksSummaryBoilerplate(s) {
				continue
			}
			if !strings.HasSuffix(s, ".") {
				s += "."
			}
			dup := false
			for _, prev := range kept {
				if sentenceOverlap(prev, s) >= 0.55 {
					dup = true
					break
				}
			}
			if !dup {
				kept = append(kept, s)
			}
		}
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

func splitSentences(s string) []string {
	s = collapseSpace(strings.ReplaceAll(s, "\n", " "))
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '.' && s[i] != '!' && s[i] != '?' {
			continue
		}
		if i+1 < len(s) && s[i+1] != ' ' {
			continue
		}
		out = append(out, strings.TrimSpace(s[start:i+1]))
		start = i + 1
	}
	if tail := strings.TrimSpace(s[start:]); tail != "" {
		out = append(out, tail)
	}
	return out
}

func looksSummaryBoilerplate(s string) bool {
	return containsAny(strings.ToLower(s),
		"review your account", "website", "click here", "log in",
		"manage your", "unsubscribe", "communication preferences")
}

func sentenceOverlap(a, b string) float64 {
	ta, tb := brandTokens(a), brandTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	share := 0
	for tok := range ta {
		if tb[tok] {
			share++
		}
	}
	if len(ta) < len(tb) {
		return float64(share) / float64(len(ta))
	}
	return float64(share) / float64(len(tb))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func embedsFromFacts(kept []messageFacts) []discordEmbed {
	var out []discordEmbed
	for _, f := range kept {
		title := deFirstPerson(strings.TrimSpace(f.Title))
		if title == "" && strings.TrimSpace(f.What) != "" {
			title = deFirstPerson(strings.TrimSpace(f.Who + " — " + f.What))
		}
		if title == "" {
			continue
		}
		desc := deFirstPerson(strings.TrimSpace(f.Summary))
		if desc == "" {
			desc = deFirstPerson(compileLine(f))
		}
		out = append(out, discordEmbed{
			Title:       clipRunes(title, 256),
			Description: clipRunes(desc, 2000),
			Color:       embedColor(f.Color),
		})
	}
	return out
}

func embedTitles(embeds []discordEmbed) []string {
	out := make([]string, len(embeds))
	for i, e := range embeds {
		out[i] = e.Title
	}
	return out
}

func digestTextSummary(content string, embeds []discordEmbed) string {
	var b strings.Builder
	b.WriteString(content)
	for _, e := range embeds {
		b.WriteString("\n\n**")
		b.WriteString(e.Title)
		b.WriteString("**\n")
		b.WriteString(e.Description)
	}
	return b.String()
}

func fallbackGreeting(localNow time.Time) string {
	h := localNow.Hour()
	switch {
	case h < 12:
		return "Good morning."
	case h < 17:
		return "Good afternoon."
	default:
		return "Good evening."
	}
}
