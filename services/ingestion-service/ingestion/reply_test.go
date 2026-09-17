package ingestion

import (
	"testing"

	gmailapi "google.golang.org/api/gmail/v1"
)

func TestLooksLikeReply(t *testing.T) {
	if looksLikeReply(&gmailapi.Message{Payload: &gmailapi.MessagePart{
		Headers: []*gmailapi.MessagePartHeader{{Name: "Subject", Value: "Hello"}},
	}}) {
		t.Fatal("cold inbound is not a reply")
	}
	if !looksLikeReply(&gmailapi.Message{Payload: &gmailapi.MessagePart{
		Headers: []*gmailapi.MessagePartHeader{{Name: "In-Reply-To", Value: "<abc@mail.gmail.com>"}},
	}}) {
		t.Fatal("In-Reply-To should count")
	}
	if !looksLikeReply(&gmailapi.Message{Payload: &gmailapi.MessagePart{
		Headers: []*gmailapi.MessagePartHeader{{Name: "References", Value: "<abc@mail.gmail.com>"}},
	}}) {
		t.Fatal("References should count")
	}
}

func TestThreadHasSent(t *testing.T) {
	if threadHasSent(&gmailapi.Thread{Messages: []*gmailapi.Message{
		{LabelIds: []string{"INBOX", "UNREAD"}},
	}}) {
		t.Fatal("inbox-only thread is not a reply to the owner")
	}
	if !threadHasSent(&gmailapi.Thread{Messages: []*gmailapi.Message{
		{LabelIds: []string{"SENT"}},
		{LabelIds: []string{"INBOX"}},
	}}) {
		t.Fatal("SENT in the thread means the owner wrote in it")
	}
}
