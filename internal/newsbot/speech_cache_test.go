package newsbot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type cacheTestSpeaker struct {
	calls atomic.Int32
	fail  bool
}

func (s *cacheTestSpeaker) Speech(ctx context.Context, text string) ([]byte, error) {
	s.calls.Add(1)
	if s.fail {
		return nil, fmt.Errorf("provider error")
	}
	return []byte("audio:" + text), nil
}
func TestSpeechCacheReuseAndInvalidation(t *testing.T) {
	ctx := context.Background()
	provider := &cacheTestSpeaker{}
	validate := func(ctx context.Context, path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(string(data), "audio:") {
			return fmt.Errorf("invalid audio")
		}
		return nil
	}
	cache := &cachedSpeaker{Speaker: provider, Dir: t.TempDir(), Namespace: "voice-style-one", validate: validate}
	var group sync.WaitGroup
	for i := 0; i < 5; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			data, err := cache.Speech(ctx, "same scene")
			if err != nil || string(data) != "audio:same scene" {
				t.Errorf("cache result: %s %v", data, err)
			}
		}()
	}
	group.Wait()
	if provider.calls.Load() != 1 {
		t.Fatal("concurrent/repeated requests paid twice")
	}
	// A new speaker instance (another job or restarted worker) reuses persisted audio.
	next := &cachedSpeaker{Speaker: provider, Dir: cache.Dir, Namespace: cache.Namespace, validate: validate}
	if _, err := next.Speech(ctx, "same scene"); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 1 {
		t.Fatal("cache was not durable")
	}
	if _, err := next.Speech(ctx, "changed scene"); err != nil {
		t.Fatal(err)
	}
	next.Namespace = "voice-style-two"
	if _, err := next.Speech(ctx, "same scene"); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 3 {
		t.Fatal("changed text/style reused wrong audio")
	}
	files, _ := filepath.Glob(filepath.Join(cache.Dir, "*.audio"))
	for _, file := range files {
		if err := os.WriteFile(file, []byte("corrupt"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := next.Speech(ctx, "same scene"); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 4 {
		t.Fatal("corrupted cache reused")
	}
	provider.fail = true
	if _, err := next.Speech(ctx, "new failing scene"); err == nil {
		t.Fatal("provider failure swallowed")
	}
	provider.fail = false
	if _, err := next.Speech(ctx, "new failing scene"); err != nil {
		t.Fatal(err)
	}
	if provider.calls.Load() != 6 {
		t.Fatal("failed audio cached")
	}
}
func TestSpeechCacheNamespaceIncludesVoiceAndPace(t *testing.T) {
	config := Config{AIProvider: "google", Google: ProviderConfig{APIKey: "secret", TTSModel: "gemini-3.8-flash-tts", Voice: "Kore"}}
	ctx := context.WithValue(context.Background(), speechContextKey{}, SpeechProfile{Voice: "voice_health", Style: "warm"})
	original := speechCacheNamespace(ctx, config)
	changed := context.WithValue(context.Background(), speechContextKey{}, SpeechProfile{Voice: "voice_health", Style: "faster"})
	if original == speechCacheNamespace(changed, config) {
		t.Fatal("pace change did not invalidate audio")
	}
	config.Google.TTSModel = "gemini-3.8-flash-lite-tts"
	if original == speechCacheNamespace(ctx, config) {
		t.Fatal("model change did not invalidate audio")
	}
}
func TestSpeechCacheFFmpegIntegration(t *testing.T) {
	if os.Getenv("NEWSBOT_RENDER_TEST") != "1" {
		t.Skip("FFprobe required")
	}
	cache := &cachedSpeaker{Speaker: fakeSpeaker{}, Dir: t.TempDir(), Namespace: "integration"}
	first, err := cache.Speech(context.Background(), "Scene")
	if err != nil {
		t.Fatal(err)
	}
	cache.Speaker = &rejectingSpeaker{}
	second, err := cache.Speech(context.Background(), "Scene")
	if err != nil || len(second) != len(first) {
		t.Fatal("valid audio was not reused", err)
	}
}
