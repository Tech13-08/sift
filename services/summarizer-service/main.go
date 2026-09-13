package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

type ingestedMessage struct {
	id        string
	mailbox   string
	from      string
	subject   string
	body      string
	replyToMe bool
}

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	healthPort := os.Getenv("SUMMARIZER_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "8090"
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("Postgres Connection Failed: %v", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		log.Fatalf("Postgres Ping Failed: %v", err)
	}
	if err := ensureSummarizerSchema(db); err != nil {
		log.Fatalf("schema: %v", err)
	}
	ensureVectorSchema(db)
	defer db.Close()

	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := db.PingContext(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if testEndpointEnabled() {
		healthMux.HandleFunc("/test-digest", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := runManualDigests(r.Context(), db, time.Now()); err != nil {
				log.Printf("manual digest failed: %v", err)
				http.Error(w, "digest failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		healthMux.HandleFunc("/test-recent", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			since := time.Now().Add(-24 * time.Hour)
			if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
				t, err := time.Parse(time.RFC3339, raw)
				if err != nil {
					http.Error(w, "since must be RFC3339", http.StatusBadRequest)
					return
				}
				since = t
			}
			if err := runRecentReplay(r.Context(), db, time.Since(since)); err != nil {
				log.Printf("recent replay failed: %v", err)
				http.Error(w, "digest failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
	}

	server := &http.Server{Addr: ":" + healthPort, Handler: healthMux}
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go startDigestScheduler(ctx, db)
	go startRulesInbox(ctx, db)
	go startMailRetention(ctx, db)
	go startEmbedBackfill(ctx, db)

	select {
	case err := <-errCh:
		log.Fatalf("Health server error: %v", err)
	case sig := <-sigCh:
		log.Printf("Received signal %s, shutting down summarizer", sig)
	}

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Health server shutdown error: %v", err)
	}
}

func ensureSummarizerSchema(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS body_text TEXT`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS kind TEXT`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS outcome TEXT`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS fact_who TEXT`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS fact_what TEXT`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS fact_when TEXT`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS fact_summary TEXT`,
		`CREATE TABLE IF NOT EXISTS user_mail_rules (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			rule_type TEXT NOT NULL,
			pattern TEXT NOT NULL,
			instruction TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`ALTER TABLE user_mail_rules ADD COLUMN IF NOT EXISTS instruction TEXT`,
		`ALTER TABLE user_mail_rules ADD COLUMN IF NOT EXISTS color INTEGER`,
		`CREATE INDEX IF NOT EXISTS user_mail_rules_user_idx ON user_mail_rules (user_id)`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS gmail_thread_id TEXT`,
		`ALTER TABLE ingested_messages ADD COLUMN IF NOT EXISTS in_reply_to_me BOOLEAN NOT NULL DEFAULT false`,
		`CREATE INDEX IF NOT EXISTS ingested_messages_ingested_at_idx ON ingested_messages (ingested_at)`,
		`CREATE TABLE IF NOT EXISTS discord_rule_cursors (
			discord_id TEXT PRIMARY KEY,
			last_message_id TEXT NOT NULL
		)`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func testEndpointEnabled() bool {
	switch os.Getenv("DIGEST_TEST_ENDPOINT") {
	case "1", "true", "TRUE", "yes":
		return true
	default:
		return false
	}
}
