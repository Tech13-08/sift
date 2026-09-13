package gmailbody

import (
	"encoding/base64"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	gmailapi "google.golang.org/api/gmail/v1"
)

const maxRunes = 100_000

var (
	htmlTag    = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	htmlBr     = regexp.MustCompile(`(?i)<br\s*/?>`)
	htmlBlock  = regexp.MustCompile(`(?i)</(p|div|tr|h[1-6]|li)>`)
	htmlAnyTag = regexp.MustCompile(`<[^>]+>`)
	extraSpace = regexp.MustCompile(`[ \t]+\n`)
	extraBlank = regexp.MustCompile(`\n{3,}`)
)

func PlainText(msg *gmailapi.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Payload != nil {
		if plain := strings.TrimSpace(collect(msg.Payload, "text/plain")); plain != "" {
			return clip(cleanText(plain))
		}
		if rawHTML := strings.TrimSpace(collect(msg.Payload, "text/html")); rawHTML != "" {
			return clip(cleanText(stripHTML(rawHTML)))
		}
	}
	return clip(cleanText(msg.Snippet))
}

func TextAttachmentIDs(msg *gmailapi.Message) []string {
	if msg == nil || msg.Payload == nil {
		return nil
	}
	var ids []string
	walkParts(msg.Payload, func(part *gmailapi.MessagePart) {
		if part == nil || part.Body == nil || part.Body.AttachmentId == "" || part.Body.Data != "" {
			return
		}
		if isTextMIME(part.MimeType) {
			ids = append(ids, part.Body.AttachmentId)
		}
	})
	return ids
}

func ApplyAttachmentData(msg *gmailapi.Message, attachmentID, data string) {
	if msg == nil || msg.Payload == nil || attachmentID == "" || data == "" {
		return
	}
	walkParts(msg.Payload, func(part *gmailapi.MessagePart) {
		if part != nil && part.Body != nil && part.Body.AttachmentId == attachmentID {
			part.Body.Data = data
		}
	})
}

func walkParts(part *gmailapi.MessagePart, fn func(*gmailapi.MessagePart)) {
	if part == nil {
		return
	}
	fn(part)
	for _, child := range part.Parts {
		walkParts(child, fn)
	}
}

func isTextMIME(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(strings.Split(ct, ";")[0]))
	return ct == "text/plain" || ct == "text/html"
}

func collect(part *gmailapi.MessagePart, mime string) string {
	if part == nil {
		return ""
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(part.MimeType, ";")[0]))
	if ct == mime && part.Body != nil && part.Body.Data != "" {
		if decoded := decodeGmailData(part.Body.Data); decoded != "" {
			return decoded
		}
	}
	var b strings.Builder
	for _, child := range part.Parts {
		if got := collect(child, mime); got != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(got)
		}
	}
	return b.String()
}

func decodeGmailData(data string) string {
	raw, err := base64.URLEncoding.DecodeString(data)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(data)
		if err != nil {
			return ""
		}
	}
	if !utf8.Valid(raw) {
		return strings.ToValidUTF8(string(raw), "")
	}
	return string(raw)
}

func stripHTML(s string) string {
	s = htmlTag.ReplaceAllString(s, "")
	s = htmlBr.ReplaceAllString(s, "\n")
	s = htmlBlock.ReplaceAllString(s, "\n")
	s = htmlAnyTag.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = extraSpace.ReplaceAllString(s, "\n")
	s = extraBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func clip(s string) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:maxRunes])) + "\n[truncated]"
}
