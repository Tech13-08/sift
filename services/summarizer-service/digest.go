package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	_ "time/tzdata"
)

type digestUser struct {
	ID        string
	DiscordID string
	Timezone  string
	Hour      int
	Minute    int
}

func loadDigestUsers(ctx context.Context, db *sql.DB) ([]digestUser, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, discord_id, timezone, digest_local_time
		FROM users
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []digestUser
	for rows.Next() {
		var u digestUser
		var clock time.Time
		if err := rows.Scan(&u.ID, &u.DiscordID, &u.Timezone, &clock); err != nil {
			return nil, err
		}
		u.Hour = clock.Hour()
		u.Minute = clock.Minute()
		users = append(users, u)
	}
	return users, rows.Err()
}

func locationOrUTC(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		log.Printf("invalid timezone %q, using UTC: %v", name, err)
		return time.UTC
	}
	return loc
}

func majorityLocalDate(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	now = now.In(loc)
	start := now.Add(-24 * time.Hour)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	if !start.Before(today) {
		return today
	}
	if now.Sub(today) >= today.Sub(start) {
		return today
	}
	return today.AddDate(0, 0, -1)
}

func siftedHeader(now time.Time, loc *time.Location) string {
	d := majorityLocalDate(now, loc)
	return "**Sifted Emails — " + d.Format("Monday, January 2, 2006") + "**"
}

func slotOnDate(day time.Time, loc *time.Location, hour, minute int) time.Time {
	local := day.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
}

func latestSlotOnOrBefore(now time.Time, loc *time.Location, hour, minute int) time.Time {
	today := slotOnDate(now, loc, hour, minute)
	if !now.Before(today) {
		return today
	}
	return slotOnDate(today.AddDate(0, 0, -1), loc, hour, minute)
}

func nextSlotAfter(t time.Time, loc *time.Location, hour, minute int) time.Time {
	today := slotOnDate(t, loc, hour, minute)
	if today.After(t) {
		return today
	}
	return slotOnDate(today.AddDate(0, 0, 1), loc, hour, minute)
}

func startDigestScheduler(ctx context.Context, db *sql.DB) {
	loggedNext := map[string]time.Time{}
	tick := func() {
		users, err := loadDigestUsers(ctx, db)
		if err != nil {
			log.Printf("digest scheduler failed to load users: %v", err)
			return
		}
		if len(users) == 0 {
			return
		}
		now := time.Now()
		for _, u := range users {
			loc := locationOrUTC(u.Timezone)
			if err := runMissedScheduledDigests(ctx, db, u, loc, now); err != nil && ctx.Err() == nil {
				log.Printf("digest catch-up failed for %s: %v", u.ID, err)
			}
			next := nextSlotAfter(now, loc, u.Hour, u.Minute)
			if !loggedNext[u.ID].Equal(next) {
				log.Printf("digest for user %s next at %s (%s %02d:%02d)",
					u.ID, next.Format(time.RFC3339), u.Timezone, u.Hour, u.Minute)
				loggedNext[u.ID] = next
			}
		}
	}

	tick()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick()
		}
	}
}

func runMissedScheduledDigests(ctx context.Context, db *sql.DB, u digestUser, loc *time.Location, now time.Time) error {
	due := latestSlotOnOrBefore(now, loc, u.Hour, u.Minute)
	exists, err := scheduledDigestExists(ctx, db, u.ID, due)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	last, err := latestScheduledWindowEnd(ctx, db, u.ID)
	if err != nil {
		return err
	}

	start := slotOnDate(due.AddDate(0, 0, -1), loc, u.Hour, u.Minute)
	if last.Valid {
		start = last.Time
	}
	if !start.Before(due) {
		return nil
	}
	if !last.Valid {
		pending, err := hasPendingInWindow(ctx, db, u.ID, start, due)
		if err != nil {
			return err
		}
		if !pending {
			return nil
		}
	}
	return runDigest(ctx, db, u, start, due, "scheduled")
}

func scheduledDigestExists(ctx context.Context, db *sql.DB, userID string, windowEnd time.Time) (bool, error) {
	var found bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM digests
			WHERE user_id = $1 AND kind = 'scheduled' AND window_end = $2
		)
	`, userID, windowEnd).Scan(&found)
	return found, err
}

func latestScheduledWindowEnd(ctx context.Context, db *sql.DB, userID string) (sql.NullTime, error) {
	var end sql.NullTime
	err := db.QueryRowContext(ctx, `
		SELECT max(window_end)
		FROM digests
		WHERE user_id = $1 AND kind = 'scheduled'
	`, userID).Scan(&end)
	return end, err
}

func hasPendingInWindow(ctx context.Context, db *sql.DB, userID string, start, end time.Time) (bool, error) {
	var found bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM ingested_messages
			WHERE user_id = $1
			  AND ingested_at >= $2
			  AND ingested_at < $3
		)
	`, userID, start, end).Scan(&found)
	return found, err
}

func runManualDigests(ctx context.Context, db *sql.DB, now time.Time) error {
	users, err := loadDigestUsers(ctx, db)
	if err != nil {
		return err
	}
	for _, u := range users {
		start := now.Add(-24 * time.Hour)
		last, err := latestScheduledWindowEnd(ctx, db, u.ID)
		if err != nil {
			return err
		}
		if last.Valid && last.Time.After(start) {
			start = last.Time
		}
		if err := runDigest(ctx, db, u, start, now, "manual"); err != nil {
			return err
		}
	}
	return nil
}

func runRecentReplay(ctx context.Context, db *sql.DB, lookback time.Duration) error {
	if lookback <= 0 {
		lookback = 24 * time.Hour
	}
	users, err := loadDigestUsers(ctx, db)
	if err != nil {
		return err
	}
	since := time.Now().Add(-lookback)
	for _, u := range users {
		messages, err := loadMessagesSince(ctx, db, u.ID, since)
		if err != nil {
			return err
		}
		var payload digestPayload
		if len(messages) > 0 {
			payload, err = buildDigest(ctx, db, u, messages)
			if err != nil {
				return err
			}
		}
		log.Printf("DIGEST kind=replay user=%s messages=%d embeds=%d", u.ID, len(messages), len(payload.Embeds))
		if len(messages) > 0 {
			log.Print(mailInventory(messages))
		}
		if strings.Trim(strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")), `"'`) == "" {
			log.Println("DISCORD_BOT_TOKEN unset; skipping Discord DM")
			continue
		}
		if payload.Content == "" && len(payload.Embeds) == 0 {
			payload.Content = "No mail to replay."
		}
		if err := sendDiscordDigest(ctx, u.DiscordID, payload); err != nil {
			return fmt.Errorf("discord dm: %w", err)
		}
		log.Printf("discord dm sent user=%s kind=replay", u.ID)
	}
	return nil
}

func loadMessagesSince(ctx context.Context, db *sql.DB, userID string, since time.Time) ([]ingestedMessage, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, mailbox, COALESCE(from_address, ''), COALESCE(subject, ''),
		       COALESCE(NULLIF(btrim(body_text), ''), snippet, ''),
		       COALESCE(in_reply_to_me, false)
		FROM ingested_messages
		WHERE user_id = $1
		  AND COALESCE(internal_date, ingested_at) >= $2
		ORDER BY COALESCE(internal_date, ingested_at) ASC, ingested_at ASC
	`, userID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ingestedMessage
	for rows.Next() {
		var msg ingestedMessage
		if err := rows.Scan(&msg.id, &msg.mailbox, &msg.from, &msg.subject, &msg.body, &msg.replyToMe); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func runDigest(ctx context.Context, db *sql.DB, u digestUser, start, end time.Time, kind string) error {
	messages, err := loadMessagesInWindow(ctx, db, u.ID, start, end)
	if err != nil {
		return err
	}

	var payload digestPayload
	if len(messages) > 0 {
		payload, err = buildDigest(ctx, db, u, messages)
		if err != nil {
			return err
		}
	}
	summary := payload.Summary

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var digestID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO digests (user_id, window_start, window_end, kind, status, summary)
		VALUES ($1, $2, $3, $4, 'completed', $5)
		ON CONFLICT (user_id, window_end) WHERE kind = 'scheduled' DO NOTHING
		RETURNING id::text
	`, u.ID, start, end, kind, summary).Scan(&digestID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if kind == "scheduled" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE ingested_messages
			SET digest_id = $1
			WHERE user_id = $2
			  AND ingested_at >= $3
			  AND ingested_at < $4
		`, digestID, u.ID, start, end); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Printf("\nDIGEST kind=%s user=%s window=%s .. %s messages=%d\n",
		kind, u.ID, start.Format(time.RFC3339), end.Format(time.RFC3339), len(messages))
	if summary == "" {
		fmt.Println("(no pending mail in this window)")
	} else {
		fmt.Println(summary)
	}
	if len(messages) > 0 {
		log.Print(mailInventory(messages))
	}

	if strings.Trim(strings.TrimSpace(os.Getenv("DISCORD_BOT_TOKEN")), `"'`) == "" {
		log.Println("DISCORD_BOT_TOKEN unset; skipping Discord DM")
		return nil
	}

	if payload.Content == "" && len(payload.Embeds) == 0 {
		if kind != "manual" {
			return nil
		}
		payload.Content = "No new mail."
	}
	if err := sendDiscordDigest(ctx, u.DiscordID, payload); err != nil {
		return fmt.Errorf("discord dm: %w", err)
	}
	log.Printf("discord dm sent user=%s", u.ID)
	return nil
}

func loadMessagesInWindow(ctx context.Context, db *sql.DB, userID string, start, end time.Time) ([]ingestedMessage, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id::text, mailbox, COALESCE(from_address, ''), COALESCE(subject, ''),
		       COALESCE(NULLIF(btrim(body_text), ''), snippet, ''),
		       COALESCE(in_reply_to_me, false)
		FROM ingested_messages
		WHERE user_id = $1
		  AND ingested_at >= $2
		  AND ingested_at < $3
		ORDER BY ingested_at ASC, id ASC
	`, userID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []ingestedMessage
	for rows.Next() {
		var msg ingestedMessage
		if err := rows.Scan(&msg.id, &msg.mailbox, &msg.from, &msg.subject, &msg.body, &msg.replyToMe); err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, rows.Err()
}

func mailInventory(messages []ingestedMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "All mail in this batch (%d):", len(messages))
	for _, msg := range messages {
		from := clipRunes(strings.TrimSpace(msg.from), 80)
		subject := clipRunes(strings.TrimSpace(msg.subject), 120)
		if from == "" {
			from = "(unknown sender)"
		}
		if subject == "" {
			subject = "(no subject)"
		}
		fmt.Fprintf(&b, "\n• %s — %s", from, subject)
	}
	return b.String()
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
