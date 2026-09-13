package main

import (
	"net/http"
	"testing"
)

func TestWebhookSecretMatch(t *testing.T) {
	if !webhookSecretMatch("abc", "abc") {
		t.Fatal("same secret should match")
	}
	if webhookSecretMatch("abc", "abd") {
		t.Fatal("different secret should not match")
	}
	if webhookSecretMatch("abc", "") {
		t.Fatal("empty header should not match")
	}
}

func TestAuthorizeWebhook(t *testing.T) {
	t.Setenv("WEBHOOK_SECRET", "")
	req := &http.Request{Header: http.Header{}}
	if !authorizeWebhook(req) {
		t.Fatal("unset secret should allow")
	}

	t.Setenv("WEBHOOK_SECRET", "s3cret")
	if authorizeWebhook(req) {
		t.Fatal("missing header should 401")
	}
	req.Header.Set("X-Webhook-Secret", "s3cret")
	if !authorizeWebhook(req) {
		t.Fatal("matching header should allow")
	}
}

func TestIsGoogleAuthError(t *testing.T) {
	if !isGoogleAuthError(errString("cannot fetch token: 400 invalid_grant")) {
		t.Fatal("invalid_grant")
	}
	if isGoogleAuthError(errString("connection refused")) {
		t.Fatal("network should not be auth")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
