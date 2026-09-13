package gmail

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"sift/ingestion/internal/tokencrypto"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func NewService(ctx context.Context, db *sql.DB, userID, email, clientID, clientSecret string) (*gmailapi.Service, error) {
	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("missing Google OAuth client credentials")
	}

	key, err := tokencrypto.ParseKey(os.Getenv("TOKEN_ENCRYPTION_KEY"))
	if err != nil {
		return nil, err
	}

	var storedAccess string
	var storedRefresh sql.NullString
	var expiresAt time.Time
	err = db.QueryRow(
		"SELECT access_token, refresh_token, expires_at FROM oauth_credentials WHERE user_id = $1 AND provider = 'google' AND email = $2",
		userID,
		email,
	).Scan(&storedAccess, &storedRefresh, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("database error: %v", err)
	}

	accessToken, err := tokencrypto.Decrypt(key, storedAccess)
	if err != nil {
		return nil, fmt.Errorf("access token decrypt: %w", err)
	}
	refreshStored := ""
	if storedRefresh.Valid {
		refreshStored = storedRefresh.String
	}
	refreshToken, err := tokencrypto.Decrypt(key, refreshStored)
	if err != nil {
		return nil, fmt.Errorf("refresh token decrypt: %w", err)
	}

	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiresAt,
	}

	tokenSource := &persistingTokenSource{
		src:           config.TokenSource(ctx, token),
		db:            db,
		key:           key,
		userID:        userID,
		email:         email,
		accessToken:   accessToken,
		storedAccess:  storedAccess,
		storedRefresh: refreshStored,
	}
	srv, err := gmailapi.NewService(ctx, option.WithTokenSource(tokenSource))
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve Gmail client: %v", err)
	}

	return srv, nil
}

type persistingTokenSource struct {
	src           oauth2.TokenSource
	db            *sql.DB
	key           []byte
	userID        string
	email         string
	mu            sync.Mutex
	accessToken   string
	storedAccess  string
	storedRefresh string
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.src.Token()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if tok.AccessToken == "" || tok.AccessToken == p.accessToken {
		return tok, nil
	}

	encAccess, err := tokencrypto.Encrypt(p.key, tok.AccessToken)
	if err != nil {
		log.Printf("encrypt refreshed google access token for %s: %v", p.email, err)
		return tok, nil
	}
	encRefresh := p.storedRefresh
	if tok.RefreshToken != "" {
		encRefresh, err = tokencrypto.Encrypt(p.key, tok.RefreshToken)
		if err != nil {
			log.Printf("encrypt refreshed google refresh token for %s: %v", p.email, err)
			return tok, nil
		}
	}

	_, err = p.db.Exec(
		`UPDATE oauth_credentials
		 SET access_token = $1,
		     expires_at = $2,
		     refresh_token = COALESCE(NULLIF($3, ''), refresh_token),
		     token_invalid_notified_at = NULL
		 WHERE user_id = $4 AND provider = 'google' AND email = $5 AND access_token = $6`,
		encAccess,
		tok.Expiry,
		encRefresh,
		p.userID,
		p.email,
		p.storedAccess,
	)
	if err != nil {
		log.Printf("persist refreshed google token for %s: %v", p.email, err)
		return tok, nil
	}
	p.accessToken = tok.AccessToken
	p.storedAccess = encAccess
	if encRefresh != "" {
		p.storedRefresh = encRefresh
	}
	return tok, nil
}
