package discord

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

func Run() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	token := strings.Trim(strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")), `"'`)
	if token == "" {
		log.Fatal("DISCORD_BOT_TOKEN is required")
	}
	summarizerURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SUMMARIZER_URL")), "/")
	if summarizerURL == "" {
		summarizerURL = "http://summarizer-service:8090"
	}
	healthPort := os.Getenv("DISCORD_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "8091"
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	if err := db.Ping(); err != nil {
		log.Fatalf("postgres ping: %v", err)
	}
	if err := ensureDiscordSchema(db); err != nil {
		log.Fatalf("schema: %v", err)
	}
	defer db.Close()

	svc := &Service{
		DB:            db,
		Token:         token,
		SummarizerURL: summarizerURL,
		HTTP:          &http.Client{Timeout: 3 * time.Minute},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
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
	mux.HandleFunc("/v1/dm", svc.handleDM)
	mux.HandleFunc("/v1/digest", svc.handleDigest)

	server := &http.Server{Addr: ":" + healthPort, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go svc.startGateway(ctx)
	go svc.startDMPoll(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		log.Fatalf("http server: %v", err)
	case sig := <-sigCh:
		log.Printf("signal %s, shutting down discord-service", sig)
	}

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
}

func ensureDiscordSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS discord_rule_cursors (
			discord_id TEXT PRIMARY KEY,
			last_message_id TEXT NOT NULL
		)
	`)
	return err
}

type Service struct {
	DB            *sql.DB
	Token         string
	SummarizerURL string
	HTTP          *http.Client
}

type linkedUser struct {
	ID        string
	DiscordID string
	Username  string
}

func (s *Service) loadLinkedUsers(ctx context.Context) ([]linkedUser, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id::text, discord_id, COALESCE(username, '')
		FROM users
		WHERE discord_id IS NOT NULL AND discord_id <> ''
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []linkedUser
	for rows.Next() {
		var u linkedUser
		if err := rows.Scan(&u.ID, &u.DiscordID, &u.Username); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
