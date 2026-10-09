package newsbot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProviderSelection(t *testing.T) {
	for _, tc := range []struct {
		name, text, speech, wantText, wantSpeech string
		openai, google, valid                    bool
	}{
		{"default OpenAI", "", "", "openai", "openai", true, false, true},
		{"Google only", "google", "", "google", "google", false, true, true},
		{"Gemini alias", " GEMINI ", "", "google", "google", false, true, true},
		{"Google text OpenAI speech", "google", "openai", "google", "openai", true, true, true},
		{"OpenAI text Google speech", "openai", "google", "openai", "google", true, true, true},
		{"missing speech key", "google", "openai", "google", "openai", false, true, false},
		{"missing text key", "google", "openai", "google", "openai", true, false, false},
		{"unknown text", "other", "", "other", "other", true, true, false},
		{"unknown speech", "openai", "other", "openai", "other", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{AIProvider: tc.text, TTSProvider: tc.speech}
			if tc.openai {
				c.OpenAI.APIKey = "openai-test"
			}
			if tc.google {
				c.Google.APIKey = "google-test"
			}
			if c.textProvider() != tc.wantText || c.speechProvider() != tc.wantSpeech {
				t.Fatal("wrong provider routing")
			}
			if err := c.ValidateAI(); (err == nil) != tc.valid {
				t.Fatalf("validation: %v", err)
			}
		})
	}
}

func TestLoadGoogleConfig(t *testing.T) {
	for key, value := range map[string]string{
		"DATABASE_URL": "postgres://test", "AI_PROVIDER": "google", "TTS_PROVIDER": "",
		"OPENAI_API_KEY": "", "GOOGLE_API_KEY": "", "GEMINI_API_KEY": "alias-key",
		"GOOGLE_TEXT_MODEL": "custom-text", "GOOGLE_TTS_MODEL": "custom-tts", "GOOGLE_VOICE": "Puck", "COLLECT_INTERVAL": "5m",
	} {
		t.Setenv(key, value)
	}
	c, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Google.APIKey != "alias-key" || c.Google.TextModel != "custom-text" || c.Google.TTSModel != "custom-tts" || c.Google.Voice != "Puck" {
		t.Fatal("Google configuration not loaded")
	}
	if err := c.ValidateAI(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_API_KEY", "primary-key")
	c, err = LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.Google.APIKey != "primary-key" {
		t.Fatal("GOOGLE_API_KEY must take precedence over GEMINI_API_KEY")
	}
	t.Setenv("AI_PROVIDER", "unsupported")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("accepted invalid provider")
	}
}

func googleResponse(w http.ResponseWriter, parts any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"parts": parts}}}})
}

func TestGoogleGenerateAndCheck(t *testing.T) {
	articles, script := fixture()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requests.Add(1)
		if r.URL.Path != "/models/gemini-test:generateContent" || r.Header.Get("x-goog-api-key") != "google-test-key" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("incorrect Google endpoint or credential routing")
		}
		var payload struct {
			SystemInstruction struct{ Parts []struct{ Text string } } `json:"systemInstruction"`
			Contents          []struct {
				Role  string
				Parts []struct{ Text string }
			}
			GenerationConfig struct {
				ResponseFormat struct {
					Text struct {
						MIMEType string `json:"mimeType"`
						Schema   map[string]any
					}
				} `json:"responseFormat"`
			} `json:"generationConfig"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if len(payload.SystemInstruction.Parts) != 1 || len(payload.Contents) != 1 || payload.Contents[0].Role != "user" || payload.GenerationConfig.ResponseFormat.Text.MIMEType != "APPLICATION_JSON" || payload.GenerationConfig.ResponseFormat.Text.Schema["type"] != "object" {
			t.Error("missing system instruction, input or JSON schema")
		}
		var out any = script
		if requestNumber == 2 {
			out = FactCheck{Scenes: []SceneCheck{{SceneIndex: 0, Supported: true}, {SceneIndex: 1, Supported: true}}}
		}
		b, _ := json.Marshal(out)
		// Hidden reasoning must not be included in JSON decoding; visible text may be split.
		googleResponse(w, []any{map[string]any{"thought": true, "text": "hidden reasoning"}, map[string]string{"text": string(b[:len(b)/2])}, map[string]string{"text": string(b[len(b)/2:])}})
	}))
	defer server.Close()
	ai := NewAI(Config{AIProvider: "google", Google: ProviderConfig{APIKey: "google-test-key", BaseURL: server.URL, TextModel: "gemini-test"}})
	s, err := ai.Generate(context.Background(), articles)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ai.Check(context.Background(), articles, s); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestGoogleRejectsBlockedOrIncomplete(t *testing.T) {
	for _, response := range []string{
		`{"promptFeedback":{"blockReason":"SAFETY"}}`,
		`{"candidates":[{"finishReason":"MAX_TOKENS","content":{"parts":[{"text":"{}"}]}}]}`,
		`{"candidates":[{"finishReason":"SAFETY","content":{"parts":[]}}]}`,
		`{"candidates":[]}`,
	} {
		if _, err := googleParts([]byte(response)); err == nil {
			t.Fatal("accepted blocked/incomplete Google response")
		}
	}
	var out Script
	if err := decodeAIJSON(`{} {}`, &out); err == nil {
		t.Fatal("accepted multiple JSON objects")
	}
	for _, name := range []string{"", "models/foo", "foo?key=secret", "../foo"} {
		if _, err := googlePath(name); err == nil {
			t.Fatal("accepted unsafe model path")
		}
	}
}

func TestGoogleSpeechFormats(t *testing.T) {
	wav, _ := (fakeSpeaker{}).Speech(context.Background(), "test")
	for _, tc := range []struct {
		name, model, mimeType string
		data                  []byte
	}{
		{"WAV", "gemini-3.8-flash-tts", "audio/wav", wav},
		{"PCM", "gemini-2.5-flash-preview-tts", "audio/L16;codec=pcm;rate=24000", []byte{1, 0, 2, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/models/"+tc.model+":generateContent" || r.Header.Get("x-goog-api-key") != "google-key" || r.Header.Get("Authorization") != "" {
					t.Error("wrong TTS credential or model")
				}
				var p struct {
					Contents         []struct{ Parts []struct{ Text string } }
					GenerationConfig struct {
						ResponseModalities []string
						SpeechConfig       struct {
							LanguageCode string
							VoiceConfig  struct {
								Voice               string
								PrebuiltVoiceConfig struct{ VoiceName string }
							}
						}
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Error(err)
				}
				if len(p.Contents) != 1 || p.Contents[0].Parts[0].Text != "Xin chào" || len(p.GenerationConfig.ResponseModalities) != 1 || p.GenerationConfig.ResponseModalities[0] != "AUDIO" {
					t.Error("wrong speech request")
				}
				if p.GenerationConfig.SpeechConfig.LanguageCode != "vi-VN" {
					t.Error("TTS language must be vi-VN")
				}
				v := p.GenerationConfig.SpeechConfig.VoiceConfig
				if tc.name == "WAV" && v.Voice != "Kore" || tc.name == "PCM" && v.PrebuiltVoiceConfig.VoiceName != "Kore" {
					t.Error("wrong voice configuration")
				}
				googleResponse(w, []any{map[string]any{"inlineData": map[string]string{"mimeType": tc.mimeType, "data": base64.StdEncoding.EncodeToString(tc.data)}}})
			}))
			defer server.Close()
			ai := NewAI(Config{AIProvider: "google", Google: ProviderConfig{APIKey: "google-key", BaseURL: server.URL, TTSModel: tc.model, Voice: "Kore"}})
			got, err := ai.Speech(context.Background(), "Xin chào")
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "WAV" {
				if !bytes.Equal(got, wav) {
					t.Fatal("WAV bytes changed")
				}
			} else {
				if string(got[:4]) != "RIFF" || binary.LittleEndian.Uint32(got[24:28]) != 24000 || !bytes.Equal(got[44:], tc.data) {
					t.Fatal("invalid PCM-to-WAV conversion")
				}
			}
		})
	}
	for _, tc := range []struct {
		data []byte
		mime string
	}{
		{nil, "audio/wav"}, {[]byte{1, 2}, "audio/wav"}, {[]byte{1}, "audio/l16"},
		{[]byte{1, 2}, "audio/l16;rate=bad"}, {[]byte{1, 2}, "audio/l16;channels=2"}, {[]byte{1, 2}, "image/png"},
	} {
		if _, err := googleAudio(tc.data, tc.mime); err == nil {
			t.Fatal("accepted invalid Google audio")
		}
	}
}

func TestMixedProvidersUseSeparateCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" || r.Header.Get("Authorization") != "Bearer openai-key" || r.Header.Get("x-goog-api-key") != "" {
			t.Error("speech sent to wrong provider")
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		if p["model"] != "openai-tts" || p["voice"] != "coral" {
			t.Error("wrong OpenAI speech settings")
		}
		_, _ = w.Write([]byte("fake-mp3"))
	}))
	defer server.Close()
	ai := NewAI(Config{AIProvider: "google", TTSProvider: "openai", Google: ProviderConfig{APIKey: "google-key", BaseURL: "https://unused.invalid"}, OpenAI: ProviderConfig{APIKey: "openai-key", BaseURL: server.URL, TTSModel: "openai-tts", Voice: "coral"}})
	b, err := ai.Speech(context.Background(), "Xin chào")
	if err != nil || strings.TrimSpace(string(b)) != "fake-mp3" {
		t.Fatalf("speech: %v", err)
	}
}
