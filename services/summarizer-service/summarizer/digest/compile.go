package digest

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"sift/summarizer-service/summarizer/llm"
	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
	"sift/summarizer-service/summarizer/rules"
	"strings"
	"time"
)

func BuildDigest(ctx context.Context, db *sql.DB, u model.DigestUser, messages []model.IngestedMessage) (model.DigestPayload, error) {
	if len(messages) == 0 {
		return model.DigestPayload{}, nil
	}
	ctx = withDigestLogUser(ctx, u)
	digestLogf(ctx, "DIGEST start messages=%d mailboxes=%v", len(messages), MessageMailboxes(messages))
	loc := LocationOrUTC(u.Timezone)
	now := time.Now()
	mailRules, err := rules.LoadRules(ctx, db, u.ID)
	if err != nil {
		log.Printf("load rules: %v", err)
	}
	facts := rules.ExtractAllFacts(ctx, messages, mailRules)
	mail.PersistFacts(ctx, db, messages, facts)
	for i, f := range facts {
		digestLogf(ctx, "fact id=%s kind=%s title=%q outcome=%q", messages[i].ID, f.Kind, f.Title, f.Outcome)
	}
	kept, noise := rules.ApplyRules(ctx, messages, facts, mailRules)
	digestLogf(ctx, "rules kept=%d noise=%d", len(kept), noise)
	kept = llm.CullUnimportant(ctx, kept)
	kept = CollapseRelatedFacts(kept)

	header := SiftedHeader(now, loc)
	greeting := ""
	if drafted, err := llm.DraftGreeting(ctx, now.In(loc)); err != nil {
		log.Printf("greeting: %v", err)
		greeting = FallbackGreeting(now.In(loc))
	} else {
		greeting = drafted
	}

	order := MailboxOrderByKept(MessageMailboxes(messages), kept)
	digestLogf(ctx, "mailbox order=%v (by kept count)", order)
	if len(order) > 1 {
		views := make(map[string]model.DigestPayload, len(order))
		var allEmbeds []model.Embed
		for _, mb := range order {
			subset := FactsForMailbox(kept, mb)
			views[mb] = MailboxDigestPayload(header, greeting, mb, subset, false)
			allEmbeds = append(allEmbeds, views[mb].Embeds...)
		}
		first := views[order[0]]
		return model.DigestPayload{
			Content:      first.Content,
			Embeds:       first.Embeds,
			Summary:      DigestTextSummary(header+"\n\n"+greeting, allEmbeds),
			MailboxOrder: order,
			MailboxViews: views,
		}, nil
	}

	embeds := EmbedsFromFacts(kept)
	if len(embeds) == 0 {
		embeds = []model.Embed{EmptyDigestEmbed()}
	}
	digestLogf(ctx, "digest embeds=%d titles=%q", len(embeds), EmbedTitles(embeds))
	content := header
	if greeting != "" {
		content += "\n\n" + greeting
	}
	if len(embeds) > model.EmbedsPerMessage {
		content += fmt.Sprintf("\n\n%d items · page 1/%d", len(embeds), (len(embeds)+model.EmbedsPerMessage-1)/model.EmbedsPerMessage)
	}
	return model.DigestPayload{
		Content: content,
		Embeds:  embeds,
		Summary: DigestTextSummary(content, embeds),
	}, nil
}

func MessageMailboxes(messages []model.IngestedMessage) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range messages {
		mb := strings.TrimSpace(m.Mailbox)
		if mb == "" || seen[mb] {
			continue
		}
		seen[mb] = true
		out = append(out, mb)
	}
	return out
}

// MailboxOrderByKept sorts inbox buttons by how many kept (important) items each has,
// most first. Ties keep the original first-seen order from `order`.
func MailboxOrderByKept(order []string, kept []model.MessageFacts) []string {
	if len(order) <= 1 {
		return order
	}
	counts := make(map[string]int, len(order))
	for _, f := range kept {
		mb := strings.TrimSpace(f.Mailbox)
		if mb != "" {
			counts[mb]++
		}
	}
	out := append([]string{}, order...)
	// Stable: higher kept count first; equal counts keep relative order.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && counts[out[j]] > counts[out[j-1]] {
			out[j], out[j-1] = out[j-1], out[j]
			j--
		}
	}
	return out
}

func FactsForMailbox(kept []model.MessageFacts, mailbox string) []model.MessageFacts {
	var out []model.MessageFacts
	for _, f := range kept {
		if strings.TrimSpace(f.Mailbox) == mailbox {
			out = append(out, f)
		}
	}
	return out
}

func EmptyDigestEmbed() model.Embed {
	return model.Embed{
		Title: "No important emails found",
		Color: model.DefaultEmbedColor,
	}
}

func MailboxDigestPayload(header, greeting, mailbox string, kept []model.MessageFacts, stampTitles bool) model.DigestPayload {
	if stampTitles {
		var stamped []model.MessageFacts
		for _, f := range kept {
			stamped = append(stamped, StampMailboxTitle(f, mailbox))
		}
		kept = stamped
	}
	embeds := EmbedsFromFacts(kept)
	if len(embeds) == 0 {
		embeds = []model.Embed{EmptyDigestEmbed()}
	}
	content := header
	if greeting != "" {
		content += "\n\n" + greeting
	}
	if mb := strings.TrimSpace(mailbox); mb != "" {
		content += "\n\n**" + mb + "**"
	}
	if len(embeds) > model.EmbedsPerMessage {
		content += fmt.Sprintf("\n\n%d items · page 1/%d", len(embeds), (len(embeds)+model.EmbedsPerMessage-1)/model.EmbedsPerMessage)
	}
	return model.DigestPayload{
		Content: content,
		Embeds:  embeds,
		Summary: DigestTextSummary(content, embeds),
	}
}

func OrganizeByMailbox(kept []model.MessageFacts) []model.MessageFacts {
	if len(kept) <= 1 {
		return kept
	}
	mailboxes := DigestMailboxes(kept)
	if len(mailboxes) <= 1 {
		return kept
	}
	var out []model.MessageFacts
	for _, mb := range mailboxes {
		for _, f := range kept {
			if f.Mailbox == mb {
				out = append(out, StampMailboxTitle(f, mb))
			}
		}
	}
	return out
}

func DigestMailboxes(kept []model.MessageFacts) []string {
	seen := map[string]bool{}
	var first []string
	for _, f := range kept {
		mb := strings.TrimSpace(f.Mailbox)
		if mb == "" || seen[mb] {
			continue
		}
		seen[mb] = true
		first = append(first, mb)
	}
	return MailboxOrderByKept(first, kept)
}

func StampMailboxTitle(f model.MessageFacts, mailbox string) model.MessageFacts {
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

func CollapseRelatedFacts(kept []model.MessageFacts) []model.MessageFacts {
	if len(kept) <= 1 {
		return kept
	}
	var out []model.MessageFacts
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

func isPaymentFact(f model.MessageFacts) bool {
	blob := strings.ToLower(f.Title + " " + f.Summary + " " + f.What)
	return mail.LooksMoneyEvent(blob) || model.ContainsAny(blob, "payment", "paid your", "bill")
}

func samePaymentEvent(a, b model.MessageFacts) bool {
	if !isPaymentFact(a) || !isPaymentFact(b) {
		return false
	}
	ka, kb := paymentWhoKey(a), paymentWhoKey(b)
	if ka != "" && ka == kb {
		return true
	}
	return shareBrandToken(a, b)
}

func paymentWhoKey(f model.MessageFacts) string {
	who := strings.ToLower(model.CollapseSpace(f.Who))
	if who == "" || mail.GenericAccountName(who) {
		return ""
	}
	return who
}

func shareBrandToken(a, b model.MessageFacts) bool {
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
		if len(part) < 5 || stop[part] || mail.GenericAccountName(part) {
			continue
		}
		out[part] = true
	}
	return out
}

func mergePaymentFacts(a, b model.MessageFacts) model.MessageFacts {
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
	if s == "" || mail.GenericAccountName(s) {
		return false
	}
	low := strings.ToLower(s)
	return !strings.Contains(low, "please") && !strings.Contains(s, ",")
}

func preferredPaymentTitle(a, b model.MessageFacts) string {
	score := func(f model.MessageFacts) int {
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
	return model.FirstNonEmpty(b.Title, a.Title)
}

func preferredWho(a, b model.MessageFacts) string {
	if usableWho(b.Who) {
		return strings.TrimSpace(b.Who)
	}
	if usableWho(a.Who) {
		return strings.TrimSpace(a.Who)
	}
	return model.FirstNonEmpty(strings.TrimSpace(b.Who), strings.TrimSpace(a.Who))
}

func clumpPaymentSummary(a, b model.MessageFacts) string {
	blob := a.Summary + " " + b.Summary + " " + a.Title + " " + b.Title
	who := preferredWho(a, b)
	amt := mail.MoneyAmount(blob)
	forWhat := paymentForWhat(a, b, who)
	received := model.ContainsAny(strings.ToLower(blob), "received", "receipt")
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

func paymentForWhat(a, b model.MessageFacts, who string) string {
	var best string
	for _, src := range []string{a.Title, b.Title, a.Summary, b.Summary} {
		for _, m := range forWhatPat.FindAllStringSubmatch(src, -1) {
			w := model.CollapseSpace(strings.Trim(m[1], " .,"))
			if w == "" || len(w) > 60 {
				continue
			}
			low := strings.ToLower(w)
			if who != "" && strings.Contains(low, strings.ToLower(who)) {
				continue
			}
			if model.ContainsAny(low, paymentForSkip...) || strings.Contains(w, "$") || strings.Contains(low, "amount") {
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
	return model.CollapseSpace(m[1])
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
	s = model.CollapseSpace(strings.ReplaceAll(s, "\n", " "))
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
	return model.ContainsAny(strings.ToLower(s),
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

func EmbedsFromFacts(kept []model.MessageFacts) []model.Embed {
	var out []model.Embed
	for _, f := range kept {
		title := llm.DeFirstPerson(strings.TrimSpace(f.Title))
		if title == "" && strings.TrimSpace(f.What) != "" {
			title = llm.DeFirstPerson(strings.TrimSpace(f.Who + " - " + f.What))
		}
		if title == "" {
			continue
		}
		desc := llm.DeFirstPerson(strings.TrimSpace(f.Summary))
		if desc == "" {
			desc = llm.DeFirstPerson(mail.CompileLine(f))
		}
		out = append(out, model.Embed{
			Title:       model.ClipRunes(title, 256),
			Description: model.ClipRunes(desc, 2000),
			Color:       rules.EmbedColor(f.Color),
		})
	}
	return out
}

func EmbedTitles(embeds []model.Embed) []string {
	out := make([]string, len(embeds))
	for i, e := range embeds {
		out[i] = e.Title
	}
	return out
}

func DigestTextSummary(content string, embeds []model.Embed) string {
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

func FallbackGreeting(localNow time.Time) string {
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
