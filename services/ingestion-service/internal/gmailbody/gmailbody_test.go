package gmailbody

import (
	"encoding/base64"
	"strings"
	"testing"

	gmailapi "google.golang.org/api/gmail/v1"
)

func TestPlainTextPrefersPlainPart(t *testing.T) {
	plain := base64.URLEncoding.EncodeToString([]byte("We regret to inform you that we will not be moving forward."))
	html := base64.URLEncoding.EncodeToString([]byte("<p>Promo 50% off Uber Eats</p>"))
	msg := &gmailapi.Message{
		Snippet: "Promo 50% off",
		Payload: &gmailapi.MessagePart{
			MimeType: "multipart/alternative",
			Parts: []*gmailapi.MessagePart{
				{MimeType: "text/plain", Body: &gmailapi.MessagePartBody{Data: plain}},
				{MimeType: "text/html", Body: &gmailapi.MessagePartBody{Data: html}},
			},
		},
	}
	got := PlainText(msg)
	if !strings.Contains(got, "will not be moving forward") {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(got, "Uber Eats") {
		t.Fatalf("used html instead of plain: %q", got)
	}
}

func TestPlainTextStripsHTMLWhenNoPlain(t *testing.T) {
	html := base64.RawURLEncoding.EncodeToString([]byte("<html><body><p>Interview Tuesday 2pm</p></body></html>"))
	msg := &gmailapi.Message{
		Payload: &gmailapi.MessagePart{
			MimeType: "text/html",
			Body:     &gmailapi.MessagePartBody{Data: html},
		},
	}
	got := PlainText(msg)
	if strings.Contains(got, "<p>") {
		t.Fatalf("left tags: %q", got)
	}
	if !strings.Contains(got, "Interview Tuesday 2pm") {
		t.Fatalf("got %q", got)
	}
}

func TestPlainTextNestedMultipart(t *testing.T) {
	plain := base64.URLEncoding.EncodeToString([]byte("Offer details inside."))
	msg := &gmailapi.Message{
		Payload: &gmailapi.MessagePart{
			MimeType: "multipart/mixed",
			Parts: []*gmailapi.MessagePart{
				{
					MimeType: "multipart/alternative",
					Parts: []*gmailapi.MessagePart{
						{MimeType: "text/plain", Body: &gmailapi.MessagePartBody{Data: plain}},
					},
				},
			},
		},
	}
	if got := PlainText(msg); got != "Offer details inside." {
		t.Fatalf("got %q", got)
	}
}

func TestTextAttachmentIDsAndApply(t *testing.T) {
	msg := &gmailapi.Message{
		Payload: &gmailapi.MessagePart{
			MimeType: "multipart/alternative",
			Parts: []*gmailapi.MessagePart{
				{MimeType: "text/plain", Body: &gmailapi.MessagePartBody{AttachmentId: "att1"}},
				{MimeType: "image/png", Body: &gmailapi.MessagePartBody{AttachmentId: "img1"}},
			},
		},
	}
	ids := TextAttachmentIDs(msg)
	if len(ids) != 1 || ids[0] != "att1" {
		t.Fatalf("ids=%v", ids)
	}
	ApplyAttachmentData(msg, "att1", base64.URLEncoding.EncodeToString([]byte("We will not be moving forward.")))
	if got := PlainText(msg); !strings.Contains(got, "will not be moving forward") {
		t.Fatalf("got %q", got)
	}
}
