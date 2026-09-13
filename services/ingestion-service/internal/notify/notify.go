package notify

import (
	"context"
	"database/sql"
	"log"
	"strings"

	"sift/ingestion/internal/discorddm"
)

func DeadGmail(ctx context.Context, db *sql.DB, userID, email string) {
	email = strings.TrimSpace(email)
	if userID == "" || email == "" {
		return
	}

	var discordID, mailbox string
	err := db.QueryRowContext(ctx, `
		UPDATE oauth_credentials AS c
		SET token_invalid_notified_at = NOW()
		FROM users u
		WHERE c.user_id = u.id
		  AND c.user_id = $1
		  AND c.provider = 'google'
		  AND lower(c.email) = lower($2)
		  AND c.token_invalid_notified_at IS NULL
		RETURNING u.discord_id, c.email
	`, userID, email).Scan(&discordID, &mailbox)
	if err == sql.ErrNoRows {
		return
	}
	if err != nil {
		log.Printf("dead gmail notice claim failed for %s: %v", email, err)
		return
	}

	if err := discorddm.Send(ctx, discordID, discorddm.DeadGmailMessage(mailbox)); err != nil {
		log.Printf("dead gmail discord dm failed for %s: %v", mailbox, err)
		if _, resetErr := db.ExecContext(ctx, `
			UPDATE oauth_credentials
			SET token_invalid_notified_at = NULL
			WHERE user_id = $1 AND provider = 'google' AND lower(email) = lower($2)
		`, userID, mailbox); resetErr != nil {
			log.Printf("dead gmail notice unclaim failed for %s: %v", mailbox, resetErr)
		}
		return
	}
	log.Printf("dead gmail discord dm sent mailbox=%s", mailbox)
}
