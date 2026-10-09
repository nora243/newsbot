package newsbot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTopicSpeechPayload(t *testing.T) {
	wav, _ := (fakeSpeaker{}).Speech(context.Background(), "test")
	var created atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "key" {
			t.Error("wrong key")
		}
		if r.URL.Path == "/voices" {
			created.Add(1)
			// Inspect REST names directly to catch accidental SDK field spelling.
			var raw map[string]any
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Fatal(err)
			}
			v := raw["voice"].(map[string]any)
			if raw["store"] != true || v["type"] != "prompted" || v["model"] != "gemini-3.8-flash-tts" || v["language_code"] != "vi-VN" || v["prompted"].(map[string]any)["input"] != defaultSpeechProfile("HEALTH").Prompt {
				t.Error("wrong voice design request")
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "voice_health_test"})
			return
		}
		var p struct {
			Contents []struct {
				Parts []struct {
					Text     string
					Metadata struct{ Style string } `json:"speech_metadata"`
				}
			}
			GenerationConfig struct {
				SpeechConfig struct{ VoiceConfig struct{ Voice string } }
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Fatal(err)
		}
		if p.Contents[0].Parts[0].Text != "Lời đọc gốc." || p.Contents[0].Parts[0].Metadata.Style != defaultSpeechProfile("HEALTH").Style || p.GenerationConfig.SpeechConfig.VoiceConfig.Voice != "voice_health_test" {
			t.Error("narration/style/voice mismatch")
		}
		googleResponse(w, []any{map[string]any{"inlineData": map[string]string{"mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wav)}}})
	}))
	defer server.Close()
	ai := NewAI(Config{AIProvider: "google", TTSAutoStyle: true, Google: ProviderConfig{APIKey: "key", BaseURL: server.URL, TTSModel: "gemini-3.8-flash-tts", Voice: "Kore"}})
	id, err := ai.createDesignedVoice(context.Background(), "HEALTH", defaultSpeechProfile("HEALTH").Prompt)
	if err != nil {
		t.Fatal(err)
	}
	p := defaultSpeechProfile("HEALTH")
	p.Voice = id
	ctx := context.WithValue(context.Background(), speechContextKey{}, p)
	for i := 0; i < 2; i++ {
		if _, err = ai.Speech(ctx, "Lời đọc gốc."); err != nil {
			t.Fatal(err)
		}
	}
	if created.Load() != 1 {
		t.Fatal("voice recreated per scene")
	}
}

func TestSpeechProfileModes(t *testing.T) {
	for _, category := range speechCategories {
		profile := defaultSpeechProfile(category)
		if profile.Prompt == "" || profile.Style == "" {
			t.Fatal(category)
		}
	}
	if defaultSpeechProfile("HEALTH").Style == defaultSpeechProfile("TECHNOLOGY").Style {
		t.Fatal("topics have identical delivery")
	}
	for _, tc := range []Config{
		{AIProvider: "openai", TTSAutoStyle: true},
		{AIProvider: "google", TTSAutoStyle: false, Google: ProviderConfig{TTSModel: "gemini-3.8-flash-tts"}},
		{AIProvider: "google", TTSAutoStyle: true, Google: ProviderConfig{TTSModel: "gemini-2.5-flash-preview-tts"}},
	} {
		ctx, err := NewAI(tc).prepareSpeech(context.Background(), nil, "HEALTH")
		if err != nil || ctx.Value(speechContextKey{}) != nil {
			t.Fatalf("unexpected profile: %v", err)
		}
	}
	ai := NewAI(Config{AIProvider: "google", TTSAutoStyle: true, Google: ProviderConfig{TTSModel: "gemini-3.8-flash-tts", Voice: "Kore"}})
	ctx, err := ai.prepareSpeech(context.Background(), nil, "HEALTH")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Value(speechContextKey{}).(SpeechProfile).Voice != "Kore" {
		t.Fatal("prebuilt mode lost voice")
	}
}

func TestDesignedVoiceCacheIntegration(t *testing.T) {
	dsn := os.Getenv("NEWSBOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated database required")
	}
	ctx := context.Background()
	store, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.DB.Close()
	if err = store.Migrate(ctx, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "voice_cached_test"})
	}))
	defer server.Close()
	ai := NewAI(Config{Google: ProviderConfig{APIKey: "unique-" + newID(), BaseURL: server.URL, TTSModel: "gemini-3.8-flash-tts"}})
	prompt := "test-" + newID()
	for i := 0; i < 2; i++ {
		id, err := ai.designedVoice(ctx, store, "HEALTH", prompt)
		if err != nil || !strings.HasPrefix(id, "voice_") {
			t.Fatalf("cache: %s %v", id, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("cache did not reuse voice")
	}
	if _, err = ai.designedVoice(ctx, store, "HEALTH", prompt+"changed"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("prompt change failed to invalidate cache")
	}
}

func TestSpeechCategoryUsesScriptAndRejectsUnknown(t *testing.T) {
	response := "HEALTH"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Contents []struct{ Parts []struct{ Text string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload.Contents[0].Parts[0].Text, "Tin sức khỏe") {
			t.Error("classifier did not receive script")
		}
		googleResponse(w, []any{map[string]string{"text": `{"category":"` + response + `"}`}})
	}))
	defer server.Close()
	ai := NewAI(Config{AIProvider: "google", Google: ProviderConfig{APIKey: "key", BaseURL: server.URL, TextModel: "gemini-test"}})
	script := Script{Title: "Tin sức khỏe"}
	if category, err := ai.speechCategory(context.Background(), script); err != nil || category != "HEALTH" {
		t.Fatalf("category: %s %v", category, err)
	}
	response = "NOT_A_TOPIC"
	if _, err := ai.speechCategory(context.Background(), script); err == nil {
		t.Fatal("unknown category accepted")
	}
}
