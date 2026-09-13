package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"sift/ingestion/internal/gmailbody"
	"sift/ingestion/internal/notify"
	gmailprovider "sift/ingestion/internal/providers/gmail"
	"sift/ingestion/internal/schema"
	"sift/ingestion/internal/tokencrypto"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	gmailapi "google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
)

var (
	db           *sql.DB
	rdb          *redis.Client
	ctx          context.Context
	cancelCtx    context.CancelFunc
	wg           sync.WaitGroup
	serviceCache = make(map[string]*cachedService)
	cacheLock    sync.RWMutex
)

const (
	gmailServiceCacheTTL       = 30 * time.Minute
	gmailServiceCacheSweepFreq = 5 * time.Minute
	gmailServiceCacheTouchFreq = 1 * time.Minute
)

type cachedService struct {
	service      *gmailapi.Service
	expiresUnix  int64
	lastUsedUnix int64
}

type PubSubPayload struct {
	Message struct {
		Data string `json:"data"`
	} `json:"message"`
}

type GmailPushData struct {
	EmailAddress string    `json:"emailAddress"`
	HistoryID    HistoryID `json:"historyId"`
}

type HistoryID string

func (h *HistoryID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*h = ""
		return nil
	}

	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		*h = HistoryID(asString)
		return nil
	}

	var asNumber json.Number
	if err := json.Unmarshal(data, &asNumber); err == nil {
		*h = HistoryID(asNumber.String())
		return nil
	}

	return fmt.Errorf("historyId must be string or number")
}

func main() {
	ctx, cancelCtx = context.WithCancel(context.Background())
	defer cancelCtx()

	if err := godotenv.Load(); err != nil {
		log.Println("Warning: No .env file found, using system env")
	}

	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal("Postgres Connection Failed:", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		log.Fatal("Postgres Ping Failed:", err)
	}

	if _, err := tokencrypto.ParseKey(os.Getenv("TOKEN_ENCRYPTION_KEY")); err != nil {
		log.Fatal(err)
	}
	if err := schema.Ensure(ctx, db); err != nil {
		log.Fatal("schema:", err)
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	rdb = redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		log.Fatal("Redis Connection Failed:", err)
	}

	if os.Getenv("WEBHOOK_SECRET") == "" {
		log.Println("WARNING: WEBHOOK_SECRET is unset; POST /webhooks/gmail is unauthenticated")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/webhooks/gmail", handleGmailWebhook)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	errCh := make(chan error, 1)
	go func() {
		fmt.Println("Sift Ingestion Service Live on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()
	go runServiceCacheJanitor(ctx)
	go func() {
		if err := backfillEmptyBodies(ctx); err != nil {
			log.Printf("body backfill: %v", err)
		}
		if err := backfillReplyToMe(ctx); err != nil {
			log.Printf("reply-to-me backfill: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("Server error: %v", err)
	case sig := <-sigCh:
		log.Printf("Received signal %s, starting graceful shutdown", sig)
	}
	cancelCtx()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}

	drainDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(drainDone)
	}()

	select {
	case <-drainDone:
		log.Println("In-flight processing completed")
	case <-shutdownCtx.Done():
		log.Println("Shutdown timeout reached; exiting")
	}

	if err := rdb.Close(); err != nil {
		log.Printf("Redis close error: %v", err)
	}
	if err := db.Close(); err != nil {
		log.Printf("Postgres close error: %v", err)
	}
}

func handleGmailWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !authorizeWebhook(r) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fmt.Println("[Go] Gmail webhook received")

	var payload PubSubPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("Error decoding PubSub: %v", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusNoContent)

	wg.Add(1)
	go func() {
		defer wg.Done()
		processNotification(payload)
	}()
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := db.PingContext(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	if err := rdb.Ping(r.Context()).Err(); err != nil {
		http.Error(w, "redis unavailable", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func authorizeWebhook(r *http.Request) bool {
	secret := os.Getenv("WEBHOOK_SECRET")
	if secret == "" {
		return true
	}
	return webhookSecretMatch(secret, r.Header.Get("X-Webhook-Secret"))
}

func webhookSecretMatch(expected, got string) bool {
	sumExpected := sha256.Sum256([]byte(expected))
	sumGot := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(sumExpected[:], sumGot[:]) == 1
}

func processNotification(payload PubSubPayload) {
	start := time.Now()
	pushData, err := decodeGmailPushData(payload.Message.Data)
	if err != nil {
		log.Printf("Failed to decode push payload: %v", err)
		return
	}

	if pushData.EmailAddress == "" {
		log.Println("Push payload missing emailAddress")
		return
	}

	acquiredDedupe := false
	if pushData.HistoryID != "" {
		shouldProcess, dedupeErr := shouldProcessHistoryNotification(pushData.EmailAddress, string(pushData.HistoryID))
		if dedupeErr != nil {
			log.Printf("History dedupe check failed: %v", dedupeErr)
		} else if !shouldProcess {
			fmt.Printf("Duplicate webhook ignored for %s historyId=%s\n", pushData.EmailAddress, string(pushData.HistoryID))
			return
		} else {
			acquiredDedupe = true
		}
	}
	keepDedupe := false
	defer func() {
		if acquiredDedupe && !keepDedupe {
			clearHistoryNotification(pushData.EmailAddress, string(pushData.HistoryID))
		}
	}()

	var userID, lastHistoryIDStr string
	fmt.Printf("Checking watch for mailbox %s...\n", pushData.EmailAddress)
	err = db.QueryRow(
		"SELECT user_id, last_history_id FROM oauth_credentials WHERE provider = 'google' AND email = $1 AND last_history_id IS NOT NULL ORDER BY watch_expiration DESC NULLS LAST LIMIT 1",
		pushData.EmailAddress,
	).Scan(&userID, &lastHistoryIDStr)

	if err != nil {
		log.Printf("No active watch found for %s: %v", pushData.EmailAddress, err)
		return
	}

	fmt.Printf("Found User: %s with HistoryID: %s\n", userID, lastHistoryIDStr)

	lastHistoryID, err := strconv.ParseUint(lastHistoryIDStr, 10, 64)
	if err != nil {
		log.Printf("Invalid history ID format: %v", err)
		return
	}

	if pushData.HistoryID != "" {
		incomingHistoryID, parseErr := strconv.ParseUint(string(pushData.HistoryID), 10, 64)
		if parseErr == nil && incomingHistoryID <= lastHistoryID {
			fmt.Printf("No new changes for %s (incoming=%d, current=%d), skipping.\n", pushData.EmailAddress, incomingHistoryID, lastHistoryID)
			keepDedupe = true
			return
		}
	}

	srv, err := getCachedGmailService(userID, pushData.EmailAddress)
	if err != nil {
		log.Printf("Auth failed: %v", err)
		evictCachedGmailService(userID, pushData.EmailAddress)
		if isGoogleAuthError(err) {
			notify.DeadGmail(ctx, db, userID, pushData.EmailAddress)
		}
		return
	}

	historyRecords, latestHistoryID, err := listGmailHistory(srv, lastHistoryID)
	if err != nil && isGoogleAuthError(err) {
		evictCachedGmailService(userID, pushData.EmailAddress)
		srv, err = getCachedGmailService(userID, pushData.EmailAddress)
		if err != nil {
			log.Printf("Auth failed after token reload: %v", err)
			notify.DeadGmail(ctx, db, userID, pushData.EmailAddress)
			return
		}
		historyRecords, latestHistoryID, err = listGmailHistory(srv, lastHistoryID)
	}
	if err != nil {
		if isGoogleAuthError(err) {
			notify.DeadGmail(ctx, db, userID, pushData.EmailAddress)
		}
		handled, handleErr := handleHistoryFetchError(userID, pushData.EmailAddress, srv, err)
		if handleErr != nil {
			log.Printf("History fetch error handling failed: %v", handleErr)
		}
		if handled {
			return
		}
		log.Printf("History fetch failed: %v", err)
		return
	}

	persistFailed := false
	for _, h := range historyRecords {
		for _, msgAdded := range h.MessagesAdded {
			if msgAdded.Message == nil || msgAdded.Message.Id == "" {
				log.Printf("History record missing message id for %s", pushData.EmailAddress)
				persistFailed = true
				continue
			}

			msg, err := fetchGmailMessage(srv, msgAdded.Message.Id)
			if err != nil {
				log.Printf("Messages.Get failed for %s: %v", msgAdded.Message.Id, err)
				persistFailed = true
				continue
			}

			if hasLabel(msg.LabelIds, "SPAM") || hasLabel(msg.LabelIds, "SENT") || hasLabel(msg.LabelIds, "DRAFT") || hasLabel(msg.LabelIds, "TRASH") {
				fmt.Printf("[priority_skip] message_id=%s mailbox=%s labels=%v\n", msg.Id, pushData.EmailAddress, msg.LabelIds)
				continue
			}

			replyToMe := detectReplyToMe(srv, msg)
			if err := insertIngestedMessage(userID, pushData.EmailAddress, msg, replyToMe); err != nil {
				log.Printf("Failed to persist message %s: %v", msg.Id, err)
				persistFailed = true
				continue
			}
			fmt.Printf("[Postgres] Ingested message_id=%s mailbox=%s subject=%q reply_to_me=%v\n", msg.Id, pushData.EmailAddress, headerValue(msg, "Subject"), replyToMe)
		}
	}

	if persistFailed {
		log.Printf("Skipping last_history_id update for %s; one or more messages were not persisted", pushData.EmailAddress)
		return
	}

	if latestHistoryID <= lastHistoryID {
		fmt.Println("No new changes since last sync, skipping.")
		keepDedupe = true
		return
	}

	newHistoryID := strconv.FormatUint(latestHistoryID, 10)
	if _, err := db.Exec(
		"UPDATE oauth_credentials SET last_history_id = $1 WHERE user_id = $2 AND provider = 'google' AND email = $3",
		newHistoryID,
		userID,
		pushData.EmailAddress,
	); err != nil {
		log.Printf("Failed to update history ID for user %s: %v", userID, err)
	}

	keepDedupe = true
	fmt.Printf("Notification processed for %s in %s\n", pushData.EmailAddress, time.Since(start).Round(time.Millisecond))
}

func decodeGmailPushData(encoded string) (GmailPushData, error) {
	var pushData GmailPushData
	if encoded == "" {
		return pushData, fmt.Errorf("empty data field")
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(encoded)
	}
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
	}
	if err != nil {
		return pushData, fmt.Errorf("base64 decode failed: %w", err)
	}

	if err := json.Unmarshal(decoded, &pushData); err != nil {
		return pushData, fmt.Errorf("payload json decode failed: %w", err)
	}

	return pushData, nil
}

func getCachedGmailService(userID, email string) (*gmailapi.Service, error) {
	key := cacheKey(userID, email)

	var expiresAt time.Time
	if err := db.QueryRow(
		"SELECT expires_at FROM oauth_credentials WHERE user_id = $1 AND provider = 'google' AND email = $2",
		userID,
		email,
	).Scan(&expiresAt); err != nil {
		return nil, fmt.Errorf("token lookup: %w", err)
	}
	expiresUnix := expiresAt.UTC().UnixMilli()

	cacheLock.RLock()
	entry, exists := serviceCache[key]
	cacheLock.RUnlock()
	if exists && entry.expiresUnix == expiresUnix {
		now := time.Now().Unix()
		lastUsed := atomic.LoadInt64(&entry.lastUsedUnix)
		if now-lastUsed >= int64(gmailServiceCacheTouchFreq/time.Second) {
			atomic.StoreInt64(&entry.lastUsedUnix, now)
		}
		fmt.Printf("[cache_hit] gmail_service key=%s\n", key)
		return entry.service, nil
	}
	if exists {
		evictCachedGmailService(userID, email)
		fmt.Printf("[cache_stale] gmail_service key=%s\n", key)
	}

	fmt.Printf("[cache_miss] gmail_service key=%s\n", key)
	createStart := time.Now()

	newSrv, err := gmailprovider.NewService(context.Background(), db, userID, email, os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"))
	if err != nil {
		return nil, err
	}

	cacheLock.Lock()
	serviceCache[key] = &cachedService{service: newSrv, expiresUnix: expiresUnix, lastUsedUnix: time.Now().Unix()}
	cacheLock.Unlock()
	fmt.Printf("[cache_store] gmail_service key=%s create_time=%s\n", key, time.Since(createStart).Round(time.Millisecond))

	return newSrv, nil
}

func isGoogleAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid_grant") || strings.Contains(msg, "cannot fetch token") {
		return true
	}
	var gErr *googleapi.Error
	if errors.As(err, &gErr) && (gErr.Code == 401 || gErr.Code == 403) {
		return true
	}
	return false
}

func evictCachedGmailService(userID, email string) {
	cacheLock.Lock()
	delete(serviceCache, cacheKey(userID, email))
	cacheLock.Unlock()
}

func runServiceCacheJanitor(ctx context.Context) {
	ticker := time.NewTicker(gmailServiceCacheSweepFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			nowUnix := time.Now().Unix()
			ttlSeconds := int64(gmailServiceCacheTTL / time.Second)
			cacheLock.Lock()
			for key, entry := range serviceCache {
				lastUsed := atomic.LoadInt64(&entry.lastUsedUnix)
				if nowUnix-lastUsed > ttlSeconds {
					delete(serviceCache, key)
				}
			}
			cacheLock.Unlock()
		}
	}
}

func handleHistoryFetchError(userID, email string, srv *gmailapi.Service, fetchErr error) (bool, error) {
	var gErr *googleapi.Error
	if !errors.As(fetchErr, &gErr) {
		return false, nil
	}

	switch gErr.Code {
	case 404, 410:
		profile, err := srv.Users.GetProfile("me").Do()
		if err != nil {
			return true, fmt.Errorf("history expired and profile sync failed: %w", err)
		}

		newHistoryID := strconv.FormatUint(profile.HistoryId, 10)
		if _, err := db.Exec(
			"UPDATE oauth_credentials SET last_history_id = $1 WHERE user_id = $2 AND provider = 'google' AND email = $3",
			newHistoryID,
			userID,
			email,
		); err != nil {
			return true, fmt.Errorf("history expired and db resync failed: %w", err)
		}

		log.Printf("History window expired for %s; resynced last_history_id to %s", email, newHistoryID)
		return true, nil

	case 401, 403:
		evictCachedGmailService(userID, email)
		notify.DeadGmail(ctx, db, userID, email)
		if _, err := db.Exec(
			"UPDATE oauth_credentials SET last_history_id = NULL WHERE user_id = $1 AND provider = 'google' AND email = $2",
			userID,
			email,
		); err != nil {
			return true, fmt.Errorf("auth invalid and state clear failed: %w", err)
		}

		log.Printf("Google auth invalid for %s; cleared last_history_id to require re-link", email)
		return true, nil
	}

	return false, nil
}

func cacheKey(userID, email string) string {
	return userID + "|" + strings.ToLower(email)
}

func historyDedupeKey(email, historyID string) string {
	return "processed_history:" + strings.ToLower(email) + ":" + historyID
}

func shouldProcessHistoryNotification(email, historyID string) (bool, error) {
	stored, err := rdb.SetNX(ctx, historyDedupeKey(email, historyID), "1", 24*time.Hour).Result()
	if err != nil {
		return false, err
	}
	return stored, nil
}

func clearHistoryNotification(email, historyID string) {
	if err := rdb.Del(ctx, historyDedupeKey(email, historyID)).Err(); err != nil {
		log.Printf("Failed to clear history dedupe for %s: %v", email, err)
	}
}

func listGmailHistory(srv *gmailapi.Service, startHistoryID uint64) ([]*gmailapi.History, uint64, error) {
	records, latest, pages, err := gmailprovider.CollectHistoryPages(startHistoryID, func(start uint64, pageToken string) (gmailprovider.HistoryPage, error) {
		call := srv.Users.History.List("me").StartHistoryId(start)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}
		res, err := call.Do()
		if err != nil {
			return gmailprovider.HistoryPage{}, err
		}
		return gmailprovider.HistoryPage{
			History:       res.History,
			HistoryID:     res.HistoryId,
			NextPageToken: res.NextPageToken,
		}, nil
	})
	if err != nil {
		return nil, 0, err
	}
	if pages > 1 {
		fmt.Printf("Gmail history pagination pages=%d records=%d latest=%d\n", pages, len(records), latest)
	}
	return records, latest, nil
}

func fetchGmailMessage(srv *gmailapi.Service, messageID string) (*gmailapi.Message, error) {
	msg, err := srv.Users.Messages.Get("me", messageID).Format("full").Do()
	if err != nil {
		return nil, err
	}
	hydrateGmailBody(srv, msg)
	return msg, nil
}

func hydrateGmailBody(srv *gmailapi.Service, msg *gmailapi.Message) {
	if srv == nil || msg == nil {
		return
	}
	for _, id := range gmailbody.TextAttachmentIDs(msg) {
		att, err := srv.Users.Messages.Attachments.Get("me", msg.Id, id).Do()
		if err != nil {
			log.Printf("attachment get message=%s id=%s: %v", msg.Id, id, err)
			continue
		}
		gmailbody.ApplyAttachmentData(msg, id, att.Data)
	}
}

func insertIngestedMessage(userID, mailbox string, msg *gmailapi.Message, replyToMe bool) error {
	var internalDate interface{}
	if msg.InternalDate > 0 {
		internalDate = time.UnixMilli(msg.InternalDate).UTC()
	}
	threadID := ""
	if msg != nil {
		threadID = strings.TrimSpace(msg.ThreadId)
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO ingested_messages (
			user_id, mailbox, gmail_message_id, from_address, subject, snippet, body_text, internal_date,
			gmail_thread_id, in_reply_to_me
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (mailbox, gmail_message_id) DO UPDATE SET
			snippet = COALESCE(EXCLUDED.snippet, ingested_messages.snippet),
			body_text = COALESCE(NULLIF(btrim(ingested_messages.body_text), ''), EXCLUDED.body_text),
			from_address = COALESCE(EXCLUDED.from_address, ingested_messages.from_address),
			subject = COALESCE(EXCLUDED.subject, ingested_messages.subject),
			gmail_thread_id = COALESCE(NULLIF(btrim(ingested_messages.gmail_thread_id), ''), EXCLUDED.gmail_thread_id),
			in_reply_to_me = ingested_messages.in_reply_to_me OR EXCLUDED.in_reply_to_me
	`, userID, strings.ToLower(mailbox), msg.Id, headerValue(msg, "From"), headerValue(msg, "Subject"), msg.Snippet, gmailbody.PlainText(msg), internalDate, nullIfBlank(threadID), replyToMe)
	return err
}

func nullIfBlank(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func backfillEmptyBodies(ctx context.Context) error {
	rows, err := db.QueryContext(ctx, `
		SELECT im.id::text, im.user_id::text, im.mailbox, im.gmail_message_id
		FROM ingested_messages im
		WHERE btrim(COALESCE(im.body_text, '')) = ''
		ORDER BY COALESCE(im.internal_date, im.ingested_at) DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id, userID, mailbox, gmailID string
	}
	var missing []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.userID, &r.mailbox, &r.gmailID); err != nil {
			return err
		}
		missing = append(missing, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	log.Printf("body backfill starting count=%d", len(missing))
	filled := 0
	for _, r := range missing {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		srv, err := getCachedGmailService(r.userID, r.mailbox)
		if err != nil {
			log.Printf("body backfill service mailbox=%s: %v", r.mailbox, err)
			continue
		}
		msg, err := fetchGmailMessage(srv, r.gmailID)
		if err != nil {
			log.Printf("body backfill get id=%s: %v", r.gmailID, err)
			continue
		}
		body := gmailbody.PlainText(msg)
		if strings.TrimSpace(body) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, `
			UPDATE ingested_messages
			SET body_text = $1, snippet = COALESCE(NULLIF(btrim(snippet), ''), $2)
			WHERE id = $3::uuid AND btrim(COALESCE(body_text, '')) = ''
		`, body, msg.Snippet, r.id); err != nil {
			log.Printf("body backfill update id=%s: %v", r.id, err)
			continue
		}
		filled++
	}
	log.Printf("body backfill done filled=%d/%d", filled, len(missing))
	return nil
}

func backfillReplyToMe(ctx context.Context) error {
	rows, err := db.QueryContext(ctx, `
		SELECT im.id::text, im.user_id::text, im.mailbox, im.gmail_message_id
		FROM ingested_messages im
		WHERE NOT im.in_reply_to_me
		  AND COALESCE(im.internal_date, im.ingested_at) > NOW() - INTERVAL '14 days'
		  AND (
		    im.gmail_thread_id IS NOT NULL
		    OR lower(COALESCE(im.subject, '')) LIKE 're:%'
		  )
		ORDER BY COALESCE(im.internal_date, im.ingested_at) DESC
		LIMIT 40
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id, userID, mailbox, gmailID string
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.userID, &r.mailbox, &r.gmailID); err != nil {
			return err
		}
		todo = append(todo, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(todo) == 0 {
		return nil
	}
	log.Printf("reply-to-me backfill starting count=%d", len(todo))
	marked := 0
	for _, r := range todo {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		srv, err := getCachedGmailService(r.userID, r.mailbox)
		if err != nil {
			log.Printf("reply-to-me backfill service mailbox=%s: %v", r.mailbox, err)
			continue
		}
		msg, err := fetchGmailMessage(srv, r.gmailID)
		if err != nil {
			log.Printf("reply-to-me backfill get id=%s: %v", r.gmailID, err)
			continue
		}
		replyToMe := detectReplyToMe(srv, msg)
		if _, err := db.ExecContext(ctx, `
			UPDATE ingested_messages
			SET gmail_thread_id = COALESCE(NULLIF(btrim(gmail_thread_id), ''), $1),
			    in_reply_to_me = in_reply_to_me OR $2
			WHERE id = $3::uuid
		`, nullIfBlank(msg.ThreadId), replyToMe, r.id); err != nil {
			log.Printf("reply-to-me backfill update id=%s: %v", r.id, err)
			continue
		}
		if replyToMe {
			marked++
		}
	}
	log.Printf("reply-to-me backfill done marked=%d/%d", marked, len(todo))
	return nil
}

func headerValue(msg *gmailapi.Message, name string) string {
	if msg == nil || msg.Payload == nil {
		return ""
	}
	for _, h := range msg.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func hasLabel(labels []string, target string) bool {
	for _, label := range labels {
		if label == target {
			return true
		}
	}
	return false
}
