package digest

import (
	"context"
	"database/sql"
	"fmt"

	"sift/summarizer-service/summarizer/model"
)

func LoadUserByDiscordID(ctx context.Context, db *sql.DB, discordID string) (model.DigestUser, error) {
	users, err := LoadDigestUsers(ctx, db)
	if err != nil {
		return model.DigestUser{}, err
	}
	for _, u := range users {
		if u.DiscordID == discordID {
			return u, nil
		}
	}
	return model.DigestUser{}, fmt.Errorf("no Sift account linked for this Discord user")
}

// Web ask path - Discord link optional.
func LoadUserByID(ctx context.Context, db *sql.DB, userID string) (model.DigestUser, error) {
	users, err := LoadDigestUsers(ctx, db)
	if err != nil {
		return model.DigestUser{}, err
	}
	for _, u := range users {
		if u.ID == userID {
			return u, nil
		}
	}
	return model.DigestUser{}, fmt.Errorf("no Sift account for user_id")
}
