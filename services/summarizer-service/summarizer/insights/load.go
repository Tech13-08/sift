package insights

import (
	"context"
	"database/sql"
	"strings"

	"sift/summarizer-service/summarizer/mail"
	"sift/summarizer-service/summarizer/model"
)

func loadRules(ctx context.Context, db *sql.DB, userID string) ([]model.MailRule, error) {
	if db == nil {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, rule_type, pattern, COALESCE(instruction, ''), COALESCE(color, 0)
		FROM user_mail_rules WHERE user_id = $1 ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rules []model.MailRule
	for rows.Next() {
		var r model.MailRule
		if err := rows.Scan(&r.ID, &r.Type, &r.Pattern, &r.Instruction, &r.Color); err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}

func keepRecapHit(h model.InsightHit, rules []model.MailRule) bool {
	if h.Kind == model.KindNotice {
		return true
	}
	if h.Kind == model.KindPromo {
		return false
	}
	msg := model.IngestedMessage{From: h.From, Subject: h.Subject, Body: h.Snippet}
	if alwaysShows(rules, msg) {
		return true
	}
	f, sure := mail.ExtractFacts(msg)
	return sure && f.Kind == model.KindNotice
}

func alwaysShows(rules []model.MailRule, msg model.IngestedMessage) bool {
	hay := strings.ToLower(msg.From + " " + msg.Subject)
	for _, r := range rules {
		if r.Type == model.RuleAlwaysShow && strings.Contains(hay, strings.ToLower(r.Pattern)) {
			return true
		}
	}
	return false
}
