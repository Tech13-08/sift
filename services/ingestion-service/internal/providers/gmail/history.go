package gmail

import (
	gmailapi "google.golang.org/api/gmail/v1"
)

type HistoryPage struct {
	History       []*gmailapi.History
	HistoryID     uint64
	NextPageToken string
}

func CollectHistoryPages(startHistoryID uint64, fetch func(start uint64, pageToken string) (HistoryPage, error)) ([]*gmailapi.History, uint64, int, error) {
	var records []*gmailapi.History
	var latest uint64
	pageToken := ""
	pages := 0
	for {
		page, err := fetch(startHistoryID, pageToken)
		if err != nil {
			return nil, 0, pages, err
		}
		pages++
		records = append(records, page.History...)
		if page.HistoryID > latest {
			latest = page.HistoryID
		}
		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}
	return records, latest, pages, nil
}
