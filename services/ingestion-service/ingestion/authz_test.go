package ingestion

import (
	"net/http"
	"net/url"
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
	req := &http.Request{Header: http.Header{}, URL: mustURL(t, "/webhooks/gmail")}
	if !authorizeWebhook(req) {
		t.Fatal("unset secret should allow")
	}

	t.Setenv("WEBHOOK_SECRET", "s3cret")
	if authorizeWebhook(req) {
		t.Fatal("missing header/token should 401")
	}
	req.Header.Set("X-Webhook-Secret", "s3cret")
	if !authorizeWebhook(req) {
		t.Fatal("matching header should allow")
	}

	req2 := &http.Request{Header: http.Header{}, URL: mustURL(t, "/webhooks/gmail?token=s3cret")}
	if !authorizeWebhook(req2) {
		t.Fatal("matching query token should allow")
	}
	req3 := &http.Request{Header: http.Header{}, URL: mustURL(t, "/webhooks/gmail?token=wrong")}
	if authorizeWebhook(req3) {
		t.Fatal("wrong query token should deny")
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
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
