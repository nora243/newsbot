package newsbot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogleVietnameseAudioGate(t *testing.T) {
	wav, _ := (fakeSpeaker{}).Speech(context.Background(), "test")
	for _, tc := range []struct {
		name         string
		rejects      int
		checkFailure bool
		wantCalls    int
		wantError    bool
	}{
		{"Vietnamese", 0, false, 1, false},
		{"French then Vietnamese", 1, false, 2, false},
		{"French repeatedly", 3, false, 3, true},
		{"checker unavailable", 0, true, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generated, checks := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var raw map[string]any
				if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
					t.Fatal(err)
				}
				if r.URL.Path == "/models/gemini-3.8-flash-tts:generateContent" {
					generated++
					speech := raw["generationConfig"].(map[string]any)["speechConfig"].(map[string]any)
					if speech["languageCode"] != "vi-VN" {
						t.Error("language not pinned")
					}
					googleResponse(w, []any{map[string]any{"inlineData": map[string]string{"mimeType": "audio/wav", "data": base64.StdEncoding.EncodeToString(wav)}}})
					return
				}
				checks++
				if r.URL.Path != "/models/audio-check:generateContent" {
					t.Error("wrong audio checker model")
				}
				parts := raw["contents"].([]any)[0].(map[string]any)["parts"].([]any)
				if len(parts) != 1 || parts[0].(map[string]any)["inlineData"] == nil {
					t.Error("checker must receive audio, not expected narration")
				}
				if tc.checkFailure {
					w.WriteHeader(503)
					return
				}
				result := map[string]any{"vietnamese_only": checks > tc.rejects, "transcript": "Xin chào Việt Nam"}
				if checks <= tc.rejects {
					result["transcript"] = "Bonjour tout le monde"
				}
				data, _ := json.Marshal(result)
				googleResponse(w, []any{map[string]string{"text": string(data)}})
			}))
			defer server.Close()
			ai := NewAI(Config{AIProvider: "google", GoogleVerifyVietnamese: true, GoogleAudioCheckModel: "audio-check", Google: ProviderConfig{APIKey: "test", BaseURL: server.URL, TTSModel: "gemini-3.8-flash-tts", TextModel: "unused", Voice: "Kore"}})
			audio, err := ai.Speech(context.Background(), "Xin chào Việt Nam")
			if (err != nil) != tc.wantError || generated != tc.wantCalls || checks != tc.wantCalls {
				t.Fatalf("error=%v generated=%d checks=%d", err, generated, checks)
			}
			if tc.wantError && len(audio) != 0 {
				t.Fatal("returned unchecked audio")
			}
			if tc.rejects == 3 {
				var blocked *AudioLanguageError
				if !errors.As(err, &blocked) || !blocked.Permanent() {
					t.Fatal("language rejection must stop the job")
				}
			}
		})
	}
}
