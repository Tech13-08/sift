package main

import (
	"context"
	"database/sql"
	"log"
	"time"
)

const (
	bodyRetention = 30 * 24 * time.Hour
	rowRetention  = 90 * 24 * time.Hour
)

func startMailRetention(ctx context.Context, db *sql.DB) {
	run := func() {
		if err := pruneIngestedMail(ctx, db, time.Now()); err != nil && ctx.Err() == nil {
			log.Printf("mail retention: %v", err)
		}
	}
	run()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

func pruneIngestedMail(ctx context.Context, db *sql.DB, now time.Time) error {
	bodyCut := now.Add(-bodyRetention)
	rowCut := now.Add(-rowRetention)
	res, err := db.ExecContext(ctx, `
		UPDATE ingested_messages
		SET body_text = NULL
		WHERE body_text IS NOT NULL
		  AND ingested_at < $1
	`, bodyCut)
	if err != nil {
		return err
	}
	bodies, _ := res.RowsAffected()
	res, err = db.ExecContext(ctx, `
		DELETE FROM ingested_messages
		WHERE ingested_at < $1
	`, rowCut)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if bodies > 0 || rows > 0 {
		log.Printf("mail retention cleared %d bodies (>30d) deleted %d rows (>90d)", bodies, rows)
	}
	return nil
}
