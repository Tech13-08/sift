package schema

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"sift/ingestion/internal/tokencrypto"
)

func Ensure(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE oauth_credentials
		ADD COLUMN IF NOT EXISTS token_invalid_notified_at TIMESTAMPTZ
	`); err != nil {
		return fmt.Errorf("oauth column: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE ingested_messages
		ADD COLUMN IF NOT EXISTS body_text TEXT
	`); err != nil {
		return fmt.Errorf("body_text column: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE ingested_messages
		ADD COLUMN IF NOT EXISTS gmail_thread_id TEXT
	`); err != nil {
		return fmt.Errorf("gmail_thread_id column: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		ALTER TABLE ingested_messages
		ADD COLUMN IF NOT EXISTS in_reply_to_me BOOLEAN NOT NULL DEFAULT false
	`); err != nil {
		return fmt.Errorf("in_reply_to_me column: %w", err)
	}
	return migratePlaintextTokens(ctx, db)
}

func migratePlaintextTokens(ctx context.Context, db *sql.DB) error {
	key, err := tokencrypto.ParseKey(os.Getenv("TOKEN_ENCRYPTION_KEY"))
	if err != nil {
		return err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT user_id, provider, email, access_token, refresh_token
		FROM oauth_credentials
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		userID, provider, email string
		access, refresh         sql.NullString
	}
	var items []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.userID, &item.provider, &item.email, &item.access, &item.refresh); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	rewritten := 0
	for _, item := range items {
		if tokencrypto.IsCiphertext(item.access.String) && (!item.refresh.Valid || tokencrypto.IsCiphertext(item.refresh.String)) {
			continue
		}
		accessPlain, err := tokencrypto.Decrypt(key, item.access.String)
		if err != nil {
			log.Printf("skip token migrate for %s: %v", item.email, err)
			continue
		}
		refreshPlain := ""
		if item.refresh.Valid {
			refreshPlain, err = tokencrypto.Decrypt(key, item.refresh.String)
			if err != nil {
				log.Printf("skip token migrate for %s: %v", item.email, err)
				continue
			}
		}
		encAccess, err := tokencrypto.Encrypt(key, accessPlain)
		if err != nil {
			return err
		}
		var encRefresh any
		if item.refresh.Valid {
			enc, err := tokencrypto.Encrypt(key, refreshPlain)
			if err != nil {
				return err
			}
			encRefresh = enc
		}
		if encAccess == item.access.String && (!item.refresh.Valid || encRefresh == item.refresh.String) {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE oauth_credentials
			SET access_token = $1, refresh_token = $2
			WHERE user_id = $3 AND provider = $4 AND email = $5
		`, encAccess, encRefresh, item.userID, item.provider, item.email); err != nil {
			return err
		}
		rewritten++
	}
	if rewritten > 0 {
		log.Printf("Encrypted %d oauth_credentials row(s) at rest", rewritten)
	}
	return nil
}
