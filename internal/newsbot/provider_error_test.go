package newsbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProviderErrorReportsCauseAndRedactsSecrets(t *testing.T) {
	key, other, admin := "google+test/key", "openai-test-key", "admin-test-token-with-more-than-24-characters"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "req-test")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"status": "INVALID_ARGUMENT", "message": fmt.Sprintf("Invalid mimeType; key=%s encoded=%s other=%s admin=%s\nmore detail", key, url.QueryEscape(key), other, admin)}})
	}))
	defer server.Close()
	ai := NewAI(Config{AIProvider: "google", AdminToken: admin, Google: ProviderConfig{APIKey: key, BaseURL: server.URL, TextModel: "gemini-test"}, OpenAI: ProviderConfig{APIKey: other}})
	articles, _ := fixture()
	_, err := ai.Generate(context.Background(), articles)
	var providerErr *ProviderHTTPError
	if !errors.As(err, &providerErr) || !providerErr.Permanent() {
		t.Fatalf("missing typed permanent error: %v", err)
	}
	message := err.Error()
	for _, secret := range []string{key, url.QueryEscape(key), other, admin} {
		if strings.Contains(message, secret) {
			t.Fatal("secret exposed in diagnostic")
		}
	}
	for _, expected := range []string{"HTTP 400", "INVALID_ARGUMENT", "Invalid mimeType", "gemini-test:generateContent", "req-test", "[REDACTED]"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("missing %s", expected)
		}
	}
	if strings.Contains(message, "\n") {
		t.Fatal("multiline provider message not sanitized")
	}
}

func TestProviderErrorDoesNotDumpUnstructuredOrOversizedBodies(t *testing.T) {
	ai := NewAI(Config{})
	for _, body := range []string{"secret raw body", `{"error":{"message":"` + strings.Repeat("x", 70000) + `"}}`} {
		response := &http.Response{StatusCode: 502, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
		err := ai.providerError(response, "google", "/models/test:generateContent", "")
		if strings.Contains(err.Error(), "secret raw body") || len(err.Error()) > 500 {
			t.Fatal("raw or excessive body logged")
		}
	}
	for _, code := range []int{400, 401, 403, 404, 422} {
		if !(&ProviderHTTPError{HTTPStatus: code}).Permanent() {
			t.Fatalf("%d should stop retry", code)
		}
	}
	for _, code := range []int{408, 429, 500, 502, 503} {
		if (&ProviderHTTPError{HTTPStatus: code}).Permanent() {
			t.Fatalf("%d should allow retry", code)
		}
	}
}
