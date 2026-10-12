package newsbot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type cachedSpeaker struct {
	Speaker   Speaker
	Dir       string
	Namespace string
	validate  func(context.Context, string) error
}

func speechCacheNamespace(ctx context.Context, config Config) string {
	provider := config.speechProvider()
	p, _ := config.provider(provider)
	profile, _ := ctx.Value(speechContextKey{}).(SpeechProfile)
	b, _ := json.Marshal([]any{"speech-v1", provider, p.BaseURL, p.APIKey, p.TTSModel, p.Voice, profile, config.GoogleVerifyVietnamese})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}
func (s *cachedSpeaker) valid(ctx context.Context, path string) error {
	if s.validate != nil {
		return s.validate(ctx, path)
	}
	_, err := probe(ctx, s.Dir, path)
	return err
}
func (s *cachedSpeaker) Speech(ctx context.Context, text string) ([]byte, error) {
	cacheDir, err := filepath.Abs(s.Dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(s.Namespace + "\x00" + text))
	key := hex.EncodeToString(hash[:])
	path := filepath.Join(cacheDir, key+".audio")
	lock, err := os.OpenFile(filepath.Join(cacheDir, key+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if info, err := os.Stat(path); err == nil && info.Size() > 0 && info.Size() <= 32<<20 {
		if err = s.valid(ctx, path); err == nil {
			data, err := os.ReadFile(path)
			if err == nil {
				slog.InfoContext(ctx, "speech cache hit", "video_id", ctx.Value(aiVideoKey{}), "cache_key", key)
				return data, nil
			}
		}
	}
	audio, err := s.Speaker.Speech(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("TTS trả audio trống")
	}
	file, err := os.CreateTemp(cacheDir, key+"-*.audio")
	if err != nil {
		return nil, err
	}
	temp := file.Name()
	defer os.Remove(temp)
	_, err = file.Write(audio)
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err = s.valid(ctx, temp); err != nil {
		return nil, err
	}
	if err = os.Rename(temp, path); err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "speech cached", "video_id", ctx.Value(aiVideoKey{}), "cache_key", key, "bytes", len(audio))
	return audio, nil
}
