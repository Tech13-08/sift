package digestlog

import (
	"context"
	"fmt"
	"log"
	"sift/summarizer-service/summarizer/model"
	"strings"
	"sync"
	"time"
)

type digestLogWho struct {
	UserID    string
	DiscordID string
	Username  string
}

type digestLogCtxKey struct{}

type digestLogEntry struct {
	Line   string
	Search string
}

const Cap = 4000 // ring buffer size for in-memory digest logs

var (
	digestLogMu      sync.Mutex
	digestLogEntries []digestLogEntry
)

func WithUser(ctx context.Context, u model.DigestUser) context.Context {
	return context.WithValue(ctx, digestLogCtxKey{}, digestLogWho{
		UserID:    u.ID,
		DiscordID: u.DiscordID,
		Username:  u.Username,
	})
}

func digestLogWhoFrom(ctx context.Context) digestLogWho {
	if ctx == nil {
		return digestLogWho{}
	}
	if w, ok := ctx.Value(digestLogCtxKey{}).(digestLogWho); ok {
		return w
	}
	return digestLogWho{}
}

func digestLogUserPrefix(ctx context.Context) string {
	w := digestLogWhoFrom(ctx)
	if w.UserID == "" && w.DiscordID == "" && w.Username == "" {
		return ""
	}
	var parts []string
	if w.Username != "" {
		parts = append(parts, "user="+w.Username)
	}
	if w.UserID != "" {
		id := w.UserID
		if len(id) > 8 {
			id = id[:8]
		}
		parts = append(parts, "id="+id)
	}
	if w.DiscordID != "" {
		parts = append(parts, "discord="+w.DiscordID)
	}
	return strings.Join(parts, " ") + " "
}

func appendDigestLog(line, extraSearch string) {
	line = strings.TrimRight(line, "\n")
	if line == "" {
		return
	}
	digestLogMu.Lock()
	defer digestLogMu.Unlock()
	stamp := time.Now().Format("15:04:05")
	stamped := stamp + " " + line
	search := strings.ToLower(stamped)
	if extra := model.CollapseSpace(extraSearch); extra != "" {
		search += " " + strings.ToLower(extra)
	}
	digestLogEntries = append(digestLogEntries, digestLogEntry{Line: stamped, Search: search})
	if len(digestLogEntries) > Cap {
		digestLogEntries = append([]digestLogEntry(nil), digestLogEntries[len(digestLogEntries)-Cap:]...)
	}
}

func Len() int {
	digestLogMu.Lock()
	defer digestLogMu.Unlock()
	return len(digestLogEntries)
}

func SnapshotEntries(limit int) []digestLogEntry {
	digestLogMu.Lock()
	defer digestLogMu.Unlock()
	if limit <= 0 || limit > len(digestLogEntries) {
		limit = len(digestLogEntries)
	}
	if limit == 0 {
		return nil
	}
	out := make([]digestLogEntry, limit)
	copy(out, digestLogEntries[len(digestLogEntries)-limit:])
	return out
}

func SnapshotEntriesSince(before int) []digestLogEntry {
	digestLogMu.Lock()
	defer digestLogMu.Unlock()
	if before < 0 {
		before = 0
	}
	if before > len(digestLogEntries) {
		before = len(digestLogEntries)
	}
	out := make([]digestLogEntry, len(digestLogEntries)-before)
	copy(out, digestLogEntries[before:])
	return out
}

func Clear() {
	digestLogMu.Lock()
	defer digestLogMu.Unlock()
	digestLogEntries = nil
}

func NormalizeFilter(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	quotes := []struct{ a, b string }{
		{`"`, `"`}, {"'", "'"}, {"“", "”"}, {"‘", "’"},
	}
	for _, q := range quotes {
		if strings.HasPrefix(s, q.a) && strings.HasSuffix(s, q.b) && len(s) >= len(q.a)+len(q.b) {
			s = strings.TrimSpace(s[len(q.a) : len(s)-len(q.b)])
			break
		}
	}
	return strings.ToLower(s)
}

func FilterFromQuery(q map[string][]string) string {
	for _, key := range []string{"filter", "q", "contains"} {
		if vals, ok := q[key]; ok && len(vals) > 0 {
			if v := NormalizeFilter(vals[0]); v != "" {
				return v
			}
		}
	}
	return ""
}

func FilterEntries(entries []digestLogEntry, filter string) []string {
	filter = NormalizeFilter(filter)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if filter != "" && !strings.Contains(e.Search, filter) {
			continue
		}
		out = append(out, e.Line)
	}
	return out
}

func Logf(ctx context.Context, format string, args ...any) {
	line := digestLogUserPrefix(ctx) + fmt.Sprintf(format, args...)
	appendDigestLog(line, "")
	log.Print(line)
}

func LogDecide(ctx context.Context, action, stage, from, subject, why, body string) {
	from = model.ClipRunes(model.CollapseSpace(from), 40)
	subject = model.ClipRunes(model.CollapseSpace(subject), 80)
	why = model.ClipRunes(model.CollapseSpace(why), 240)
	if why == "" {
		why = "-"
	}
	line := digestLogUserPrefix(ctx) + fmt.Sprintf(
		"decide %s stage=%s from=%q subject=%q why=%q",
		action, stage, from, subject, why,
	)
	extra := strings.Join([]string{from, subject, why, model.CollapseSpace(body)}, " ")
	appendDigestLog(line, extra)
	log.Print(line)
}

func AppendLog(line, extraSearch string) {
	appendDigestLog(line, extraSearch)
}
