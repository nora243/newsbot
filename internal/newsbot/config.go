package newsbot

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	GoogleVerifyVietnamese                                 bool
	GoogleAudioCheckModel                                  string
	TTSAutoStyle, GoogleVoiceDesign                        bool
	SpeechProfiles                                         map[string]SpeechProfile
	DatabaseURL, RedisURL, AdminToken, OutputDir, HTTPAddr string
	AIProvider, TTSProvider                                string
	OpenAI, Google                                         ProviderConfig
	CollectInterval                                        time.Duration
}

type ProviderConfig struct {
	APIKey, BaseURL, TextModel, TTSModel, Voice string
}

func providerName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "openai"
	}
	if name == "gemini" {
		return "google"
	}
	return name
}

func (c Config) textProvider() string { return providerName(c.AIProvider) }
func (c Config) speechProvider() string {
	if strings.TrimSpace(c.TTSProvider) == "" {
		return c.textProvider()
	}
	return providerName(c.TTSProvider)
}
func (c Config) provider(name string) (ProviderConfig, error) {
	switch providerName(name) {
	case "openai":
		return c.OpenAI, nil
	case "google":
		return c.Google, nil
	default:
		return ProviderConfig{}, fmt.Errorf("provider %q không hợp lệ; dùng openai hoặc google", name)
	}
}

// ValidateAI checks only selected providers; unused API keys are optional.
func (c Config) ValidateAI() error {
	for _, name := range []string{c.textProvider(), c.speechProvider()} {
		p, err := c.provider(name)
		if err != nil {
			return err
		}
		if strings.TrimSpace(p.APIKey) == "" {
			if name == "google" {
				return fmt.Errorf("thiếu GOOGLE_API_KEY (hoặc GEMINI_API_KEY)")
			}
			return fmt.Errorf("thiếu OPENAI_API_KEY")
		}
	}
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func LoadConfig() (Config, error) {
	c := Config{
		DatabaseURL: os.Getenv("DATABASE_URL"), RedisURL: env("REDIS_URL", "redis://localhost:6379/0"),
		AdminToken:  os.Getenv("ADMIN_TOKEN"),
		AIProvider:  providerName(env("AI_PROVIDER", "openai")),
		TTSProvider: strings.TrimSpace(os.Getenv("TTS_PROVIDER")),
		OpenAI: ProviderConfig{APIKey: os.Getenv("OPENAI_API_KEY"), BaseURL: env("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			TextModel: env("OPENAI_TEXT_MODEL", "gpt-4o-mini"), TTSModel: env("OPENAI_TTS_MODEL", "gpt-4o-mini-tts"), Voice: env("OPENAI_VOICE", "coral")},
		Google: ProviderConfig{APIKey: env("GOOGLE_API_KEY", os.Getenv("GEMINI_API_KEY")), BaseURL: env("GOOGLE_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
			TextModel: env("GOOGLE_TEXT_MODEL", "gemini-3.8-flash"), TTSModel: env("GOOGLE_TTS_MODEL", "gemini-3.8-flash-tts"), Voice: env("GOOGLE_VOICE", "Kore")},
		OutputDir: env("OUTPUT_DIR", "data/videos"),
		HTTPAddr:  env("HTTP_ADDR", "127.0.0.1:8080"),
	}
	var err error
	c.TTSAutoStyle, err = strconv.ParseBool(env("TTS_AUTO_STYLE", "true"))
	if err != nil {
		return c, fmt.Errorf("TTS_AUTO_STYLE phải là true/false")
	}
	c.GoogleVoiceDesign, err = strconv.ParseBool(env("GOOGLE_VOICE_DESIGN", "true"))
	if err != nil {
		return c, fmt.Errorf("GOOGLE_VOICE_DESIGN phải là true/false")
	}
	c.GoogleVerifyVietnamese, err = strconv.ParseBool(env("GOOGLE_VERIFY_VIETNAMESE", "true"))
	if err != nil {
		return c, fmt.Errorf("GOOGLE_VERIFY_VIETNAMESE phải là true/false")
	}
	c.GoogleAudioCheckModel = env("GOOGLE_AUDIO_CHECK_MODEL", c.Google.TextModel)
	c.SpeechProfiles = map[string]SpeechProfile{}
	for _, category := range speechCategories {
		profile := defaultSpeechProfile(category)
		profile.Voice = os.Getenv("GOOGLE_VOICE_ID_" + category)
		profile.Prompt = env("GOOGLE_VOICE_PROMPT_"+category, profile.Prompt)
		profile.Style = env("GOOGLE_SPEECH_STYLE_"+category, profile.Style)
		c.SpeechProfiles[category] = profile
	}
	if _, err = c.provider(c.textProvider()); err != nil {
		return c, err
	}
	if _, err = c.provider(c.speechProvider()); err != nil {
		return c, err
	}
	c.CollectInterval, err = time.ParseDuration(env("COLLECT_INTERVAL", "5m"))
	if err != nil || c.CollectInterval < time.Minute {
		return c, fmt.Errorf("COLLECT_INTERVAL phải từ 1m trở lên")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("thiếu DATABASE_URL")
	}
	return c, nil
}
