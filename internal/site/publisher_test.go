package site

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookValidationAndDeduplication(t *testing.T) {
	p, err := newPublisher(PublisherOptions{
		ContentRepo: "git@github.com:owner/content.git", ContentBranch: "master",
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
	request.Header.Set("X-Hub-Signature-256", webhookSignature(payload, "secret"))
	response := httptest.NewRecorder()
	p.webhook(response, request)
	if response.Code != http.StatusAccepted || len(p.trigger) != 1 {
		t.Fatalf("valid webhook: status=%d queued=%d", response.Code, len(p.trigger))
	}

	request = httptest.NewRequest(http.MethodPost, "/hooks/content", strings.NewReader(string(payload)))
	request.Header.Set("X-GitHub-Event", "push")
	request.Header.Set("X-GitHub-Delivery", "delivery-1")
	request.Header.Set("X-Hub-Signature-256", webhookSignature(payload, "secret"))
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

func TestWebhookSignatureMatchesGitHubVector(t *testing.T) {
	const want = "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	if got := webhookSignature([]byte("Hello, World!"), "It's a Secret to Everybody"); got != want {
		t.Fatalf("webhook signature = %q, want %q", got, want)
	}
}

func TestWebhookSignatureDiagnostics(t *testing.T) {
	payload := []byte("payload")
	valid := webhookSignature(payload, "secret")
	tests := []struct {
		name   string
		header string
		want   string
		valid  bool
	}{
		{name: "missing", want: "signature_missing"},
		{name: "scheme", header: "sha1=00", want: "signature_scheme_not_sha256"},
		{name: "length", header: "sha256=00", want: "signature_hex_length_not_64"},
		{name: "hex", header: "sha256=" + strings.Repeat("z", 64), want: "signature_hex_invalid"},
		{name: "mismatch", header: "sha256=" + strings.Repeat("0", 64), want: "signature_mismatch"},
		{name: "valid", header: valid, want: "ok", valid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := checkWebhookSignature(payload, test.header, "secret")
			if got.Reason != test.want || got.Valid != test.valid {
				t.Fatalf("check = reason %q valid %t, want reason %q valid %t", got.Reason, got.Valid, test.want, test.valid)
			}
			if got.Expected != valid {
				t.Fatalf("expected signature = %q, want %q", got.Expected, valid)
			}
		})
	}
}

func TestWebhookSecretSummaryDoesNotRevealSecret(t *testing.T) {
	const secret = "distinctive-secret\n"
	summary := webhookSecretSummary(secret)
	if strings.Contains(summary, secret) {
		t.Fatal("secret summary contains the raw secret")
	}
	for _, want := range []string{
		"secret_bytes=19",
		"secret_runes=19",
		"secret_trimmed_bytes=18",
		"secret_boundary_whitespace=true",
		"secret_control_chars=true",
		"secret_utf8_valid=true",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q does not contain %q", summary, want)
		}
	}
}

func TestPublisherRejectsHTTPSRepository(t *testing.T) {
	_, err := newPublisher(PublisherOptions{
		ContentRepo: "https://github.com/owner/content.git", ContentBranch: "master",
		ExpectedRepo: "owner/content", WebhookSecret: "secret",
		SiteTitle: "Test site", Language: "en",
	})
	if err == nil || !strings.Contains(err.Error(), "SSH URL") {
		t.Fatalf("expected SSH URL error, got %v", err)
	}
}
