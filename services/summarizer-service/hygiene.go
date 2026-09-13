package main

import (
	"context"
	"database/sql"
	"fmt"
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

func hygieneFooter(senders []senderFreq, rules []mailRule) string {
	var lines []string
	for _, s := range senders {
		if !shouldHygieneNudge(s, rules) {
			continue
		}
		who := senderWho(s.From)
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

func shouldHygieneNudge(s senderFreq, rules []mailRule) bool {
	msg := ingestedMessage{from: s.From}
	if muted(rules, msg) || alwaysShows(rules, msg) {
		return false
	}
	if isPersonalSender(s.From) {
		return false
	}
	if s.PromoCount*2 >= s.Count && s.PromoCount > 0 {
		return true
	}
	return looksMarketingList(s.From)
}

func isPersonalSender(from string) bool {
	_, domain := splitFrom(from)
	if domain == "" {
		return false
	}
	switch domain {
	case "gmail.com", "googlemail.com", "yahoo.com", "yahoo.co.uk",
		"hotmail.com", "outlook.com", "live.com", "msn.com",
		"icloud.com", "me.com", "mac.com",
		"proton.me", "protonmail.com", "aol.com", "fastmail.com":
		return !looksMarketingList(from)
	default:
		return false
	}
}

func looksMarketingList(from string) bool {
	local, domain := splitFrom(from)
	if local == "" {
		return false
	}
	if containsAny(local, "news", "marketing", "promo", "deals", "newsletter", "offers") {
		return true
	}
	if strings.HasPrefix(domain, "email.") || strings.HasPrefix(domain, "mail.") || strings.HasPrefix(domain, "e.") {
		return true
	}
	return false
}

func splitFrom(from string) (local, domain string) {
	addr := strings.ToLower(strings.TrimSpace(from))
	if i := strings.Index(addr, "<"); i >= 0 {
		end := strings.Index(addr, ">")
		if end > i {
			addr = addr[i+1 : end]
		}
	}
	local, domain, _ = strings.Cut(addr, "@")
	return local, domain
}

func muteHint(from string) string {
	_, domain := splitFrom(from)
	if domain == "" {
		return strings.ToLower(strings.TrimSpace(senderWho(from)))
	}
	for _, p := range []string{"email.", "mail.", "e.", "g."} {
		domain = strings.TrimPrefix(domain, p)
	}
	host, _, _ := strings.Cut(domain, ".")
	if host != "" && host != "com" && host != "net" && host != "org" {
		return host
	}
	return domain
}
