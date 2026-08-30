package site

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookValidationAndDeduplication(t *testing.T) {
	p, err := newPublisher(PublisherOptions{
		ContentRepo: "https://github.com/owner/content.git", ContentBranch: "master",
		ExpectedRepo: "owner/content", WebhookSecret: "secret", StateDir: t.TempDir(),
		SiteTitle: "Test site", Language: "en",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"ref":"refs/heads/master","repository":{"full_name":"owner/content"}}`)

	request := httptest.NewRequest(http.MethodPost, "/hooks/content", strings.NewReader(string(payload)))
	request.Header.Set("X-GitHub-Event", "push")
	request.Header.Set("X-GitHub-Delivery", "delivery-1")
	request.Header.Set("X-Hub-Signature-256", signature(payload, "secret"))
	response := httptest.NewRecorder()
	p.webhook(response, request)
	if response.Code != http.StatusAccepted || len(p.trigger) != 1 {
		t.Fatalf("valid webhook: status=%d queued=%d", response.Code, len(p.trigger))
	}

	request = httptest.NewRequest(http.MethodPost, "/hooks/content", strings.NewReader(string(payload)))
	request.Header.Set("X-GitHub-Event", "push")
	request.Header.Set("X-GitHub-Delivery", "delivery-1")
	request.Header.Set("X-Hub-Signature-256", signature(payload, "secret"))
	response = httptest.NewRecorder()
	p.webhook(response, request)
	if response.Code != http.StatusAccepted || len(p.trigger) != 1 {
		t.Fatal("duplicate delivery was queued")
	}

	request = httptest.NewRequest(http.MethodPost, "/hooks/content", strings.NewReader(string(payload)))
	request.Header.Set("X-GitHub-Event", "push")
	request.Header.Set("X-Hub-Signature-256", "sha256=00")
	response = httptest.NewRecorder()
	p.webhook(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status: %d", response.Code)
	}
}

func signature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
