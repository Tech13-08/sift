package main

import (
	"log"
	"strings"

	gmailapi "google.golang.org/api/gmail/v1"
)

func looksLikeReply(msg *gmailapi.Message) bool {
	if msg == nil {
		return false
	}
	return strings.TrimSpace(headerValue(msg, "In-Reply-To")) != "" ||
		strings.TrimSpace(headerValue(msg, "References")) != ""
}

func threadHasSent(thread *gmailapi.Thread) bool {
	if thread == nil {
		return false
	}
	for _, m := range thread.Messages {
		if m != nil && hasLabel(m.LabelIds, "SENT") {
			return true
		}
	}
	return false
}

func detectReplyToMe(srv *gmailapi.Service, msg *gmailapi.Message) bool {
	if srv == nil || msg == nil || strings.TrimSpace(msg.ThreadId) == "" || !looksLikeReply(msg) {
		return false
	}
	thread, err := srv.Users.Threads.Get("me", msg.ThreadId).Format("minimal").Do()
	if err != nil {
		log.Printf("thread get id=%s: %v", msg.ThreadId, err)
		return false
	}
	return threadHasSent(thread)
}
