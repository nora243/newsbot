package newsbot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

type ProviderHTTPError struct {
	Provider                           string
	HTTPStatus                         int
	Endpoint, Code, Message, RequestID string
}

func (e *ProviderHTTPError) Error() string {
	text := fmt.Sprintf("%s AI HTTP %d, endpoint=%s", e.Provider, e.HTTPStatus, e.Endpoint)
	if e.Code != "" {
		text += ", code=" + e.Code
	}
	if e.Message != "" {
		text += ": " + e.Message
	}
	if e.RequestID != "" {
		text += ", request_id=" + e.RequestID
	}
	return text
}

func (e *ProviderHTTPError) Permanent() bool {
	return e.HTTPStatus >= 400 && e.HTTPStatus < 500 && e.HTTPStatus != 408 && e.HTTPStatus != 429
}

func (a *AI) diagnosticText(text string, extraKey string, maxRunes int) string {
	for _, secret := range []string{extraKey, a.Config.Google.APIKey, a.Config.OpenAI.APIKey, a.Config.AdminToken} {
		if secret == "" {
			continue
		}
		for _, value := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			text = strings.ReplaceAll(text, value, "[REDACTED]")
		}
	}
	text = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)), " ")
	runes := []rune(text)
	if len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "…"
	}
	return text
}

func (a *AI) providerError(response *http.Response, provider, endpoint, key string) error {
	e := &ProviderHTTPError{Provider: provider, HTTPStatus: response.StatusCode,
		Endpoint: a.diagnosticText(endpoint, key, 200), RequestID: a.diagnosticText(response.Header.Get("x-request-id"), key, 100)}
	// Read bounded, structured diagnostic fields only. Never log a raw body/request.
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(data) > 64<<10 {
		return e
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &body) != nil {
		return e
	}
	code := body.Error.Status
	if code == "" {
		code = body.Error.Type
	}
	e.Code = a.diagnosticText(code, key, 80)
	e.Message = a.diagnosticText(body.Error.Message, key, 700)
	return e
}
