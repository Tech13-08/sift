package gmail

import (
	"errors"
	"testing"

	gmailapi "google.golang.org/api/gmail/v1"
)

func TestCollectHistoryPagesWalksTokens(t *testing.T) {
	calls := 0
	records, latest, pages, err := CollectHistoryPages(100, func(start uint64, pageToken string) (HistoryPage, error) {
		calls++
		if start != 100 {
			t.Fatalf("start=%d", start)
		}
		if calls == 1 {
			if pageToken != "" {
				t.Fatalf("first page token=%q", pageToken)
			}
			return HistoryPage{
				History:       []*gmailapi.History{{Id: 101}},
				HistoryID:     150,
				NextPageToken: "p2",
			}, nil
		}
		if pageToken != "p2" {
			t.Fatalf("second page token=%q", pageToken)
		}
		return HistoryPage{
			History:   []*gmailapi.History{{Id: 160}},
			HistoryID: 180,
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || len(records) != 2 || latest != 180 {
		t.Fatalf("pages=%d records=%d latest=%d", pages, len(records), latest)
	}
}

func TestCollectHistoryPagesStopsOnError(t *testing.T) {
	boom := errors.New("boom")
	_, _, pages, err := CollectHistoryPages(1, func(uint64, string) (HistoryPage, error) {
		return HistoryPage{}, boom
	})
	if err != boom {
		t.Fatalf("err=%v", err)
	}
	if pages != 0 {
		t.Fatalf("pages=%d", pages)
	}
}
