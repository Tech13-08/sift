package rules

import (
	"context"
	"database/sql"
	"fmt"
	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
	"strings"
)

const hygieneMinCount = 4
const hygieneLookbackDays = 14

type senderFreq struct {
	From       string
	Count      int
	PromoCount int
}

func loadNoisySenders(ctx context.Context, db *sql.DB, userID string) ([]senderFreq, error) {
	if db == nil || userID == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT COALESCE(from_address, ''),
		       count(*),
		       count(*) FILTER (WHERE kind = 'promo')
		FROM ingested_messages
		WHERE user_id = $1
		  AND ingested_at > now() - ($2::text || ' days')::interval
		GROUP BY from_address
		HAVING count(*) >= $3
		ORDER BY count(*) DESC
		LIMIT 20
	`, userID, fmt.Sprintf("%d", hygieneLookbackDays), hygieneMinCount)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []senderFreq
	for rows.Next() {
		var s senderFreq
		if err := rows.Scan(&s.From, &s.Count, &s.PromoCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func HygieneFooter(senders []senderFreq, rules []model.MailRule) string {
	var lines []string
	for _, s := range senders {
		if !shouldHygieneNudge(s, rules) {
			continue
		}
		who := mail.SenderWho(s.From)
		if who == "" {
			who = strings.TrimSpace(s.From)
		}
		hint := muteHint(s.From)
		if who == "" || hint == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s has emailed you %d times in %d days. Say `mute %s` if you want them out of the digest.",
			who, s.Count, hygieneLookbackDays, hint))
		if len(lines) >= 3 {
			break
		}
	}
	return strings.Join(lines, "\n")
}

func shouldHygieneNudge(s senderFreq, rules []model.MailRule) bool {
	msg := model.IngestedMessage{From: s.From}
	if Muted(context.Background(), rules, msg) || AlwaysShows(rules, msg) {
		return false
	}
	if mail.IsPersonalSender(s.From) {
		return false
	}
	if s.PromoCount*2 >= s.Count && s.PromoCount > 0 {
		return true
	}
	return mail.LooksMarketingList(s.From)
}

func muteHint(from string) string {
	if addr := mail.SenderAddress(from); addr != "" {
		return addr
	}
	_, domain := mail.SplitFrom(from)
	if domain == "" {
		return strings.ToLower(strings.TrimSpace(mail.SenderWho(from)))
	}
	return domain
}
