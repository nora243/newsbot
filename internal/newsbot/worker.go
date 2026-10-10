package newsbot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type Collector struct {
	Store  *Store
	Client *http.Client
}

func (c *Collector) Collect(ctx context.Context) error {
	sources, err := c.Store.Sources(ctx, true)
	if err != nil {
		return err
	}
	for _, source := range sources {
		articles, fetchErr := FetchFeed(ctx, c.Client, source)
		added := 0
		if fetchErr == nil {
			for _, article := range articles {
				ok, e := c.Store.SaveArticle(ctx, article)
				if e != nil {
					fetchErr = e
					break
				}
				if ok {
					added++
				}
			}
		}
		var message *string
		if fetchErr != nil {
			m := fetchErr.Error()
			message = &m
			slog.Warn("collect failed", "source", source.Name, "error", fetchErr)
		}
		if _, err = c.Store.DB.Exec(ctx, `UPDATE news_sources SET last_fetched_at=now(),last_error=$1 WHERE id=$2`, message, source.ID); err != nil {
			return err
		}
		slog.Info("collected", "source", source.Name, "inserted", added)
	}
	return nil
}

type Worker struct {
	Store    *Store
	Redis    *redis.Client
	AI       *AI
	Renderer *Renderer
}

func (w *Worker) process(ctx context.Context, j Job) (path string, duration float64, err error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	leaseDone := make(chan struct{})
	defer func() { cancel(); <-leaseDone }()
	go func() {
		defer close(leaseDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartCtx, stop := context.WithTimeout(ctx, 10*time.Second)
				e := w.Store.Heartbeat(heartCtx, j)
				stop()
				if e != nil {
					slog.Error("lease lost", "job", j.ID, "error", e)
					cancel()
					return
				}
			}
		}
	}()
	articles, err := w.Store.JobArticles(ctx, j.VideoID)
	if err != nil {
		return "", 0, fmt.Errorf("load articles: %w", err)
	}
	selectedMedia, err := w.Store.ResolveVideoMedia(ctx, j.VideoID, w.Renderer.OutputDir)
	if err != nil {
		return "", 0, err
	}
	draft, err := w.Store.Draft(ctx, j.VideoID)
	if err != nil {
		return "", 0, err
	}
	if draft == nil {
		for i := range articles {
			article := &articles[i]
			if googleNewsArticle(article.URL) {
				return "", 0, fmt.Errorf("bài %d đang dùng URL Google News; cần URL bài gốc để lấy nội dung và media chính xác", article.ID)
			}
			if (len(strings.Fields(article.Content)) >= 70 && internationalPublisher(article.URL) == "" && !dantriURL(article.URL)) || !isFullContentSupported(article.URL) {
				continue
			}
			content, fetchErr := fetchArticleContent(ctx, article.URL)
			if fetchErr != nil {
				return "", 0, fmt.Errorf("load full article %d: %w", article.ID, fetchErr)
			}
			slog.Info("full article loaded", "video_id", j.VideoID, "article_id", article.ID, "words", len(strings.Fields(content)))
			// Preserve the original RSS text so existing evidence remains valid.
			if !strings.Contains(article.Content, content) {
				article.Content += "\n" + content
			}
			if _, err = w.Store.DB.Exec(ctx, `UPDATE news_articles SET content=$1 WHERE id=$2`, article.Content, article.ID); err != nil {
				return "", 0, err
			}
		}
	}
	repairCheck, err := w.Store.RepairCheck(ctx, j.VideoID)
	if err != nil {
		return "", 0, err
	}
	var script Script
	if draft != nil {
		script = *draft
		if repairCheck != nil {
			script, err = w.AI.RepairScenes(ctx, articles, script, *repairCheck)
		} else {
			err = script.Validate(articles)
		}
	} else {
		script, err = w.AI.Generate(ctx, articles)
	}
	if err != nil {
		var providerErr *ProviderHTTPError
		// Keep the rejected draft for diagnosis; never send it to TTS/render.
		if !errors.As(err, &providerErr) && (script.Title != "" || script.Claims != nil || script.Scenes != nil) {
			savedCheck := FactCheck{}
			if repairCheck != nil {
				savedCheck = *repairCheck
			}
			if saveErr := w.Store.SaveScript(ctx, j, script, savedCheck); saveErr != nil {
				return "", 0, saveErr
			}
		}
		return "", 0, fmt.Errorf("script generate/validate: %w", err)
	}
	check, err := w.AI.Check(ctx, articles, script)
	// Persist both successful and blocked drafts for review/debugging.
	if saveErr := w.Store.SaveScript(ctx, j, script, check); saveErr != nil {
		return "", 0, saveErr
	}
	if err != nil {
		return "", 0, fmt.Errorf("fact check: %w", err)
	}
	speechCtx := ctx
	if w.AI.Config.speechProvider() == "google" && supportsVoiceDesign(w.AI.Config.Google.TTSModel) && w.AI.Config.TTSAutoStyle {
		category, categoryErr := w.AI.speechCategory(ctx, script)
		if categoryErr != nil {
			return "", 0, fmt.Errorf("speech category: %w", categoryErr)
		}
		speechCtx, err = w.AI.prepareSpeech(ctx, w.Store, category)
		if err != nil {
			return "", 0, fmt.Errorf("voice design: %w", err)
		}
		slog.Info("speech profile selected", "video_id", j.VideoID, "category", category)
	}
	renderer := *w.Renderer
	renderer.SelectedMedia = selectedMedia
	return renderer.Render(speechCtx, j.VideoID, script, articles)
}
func (w *Worker) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := w.Store.Reap(ctx); err != nil {
			return err
		}
		j, err := w.Store.ClaimJob(ctx)
		if err != nil {
			return err
		}
		if j == nil {
			// Redis is a bounded wake-up hint; PostgreSQL remains the durable queue.
			if w.Redis != nil {
				_, err = w.Redis.BLPop(ctx, 5*time.Second, "newsbot:wake").Result()
				if err != nil && err != redis.Nil && ctx.Err() == nil {
					slog.Warn("redis unavailable; polling database", "error", err)
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
					}
				}
			} else {
				select {
				case <-ctx.Done():
				case <-time.After(5 * time.Second):
				}
			}
			continue
		}
		slog.Info("processing", "video_id", j.VideoID, "attempt", j.Attempts)
		path, duration, jobErr := w.process(ctx, *j)
		finishCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = w.Store.Finish(finishCtx, *j, path, duration, jobErr)
		cancel()
		if err != nil {
			slog.Error("finish failed", "error", err)
		}
		if jobErr != nil && err == nil {
			if delay := jobRetryDelay(j.Attempts, jobErr); delay > 0 {
				slog.Warn("video retry scheduled", "video_id", j.VideoID, "attempt", j.Attempts, "retry_after", delay.String(), "error", jobErr)
			} else {
				slog.Error("video failed", "video_id", j.VideoID, "attempt", j.Attempts, "error", jobErr)
			}
		}
	}
	return ctx.Err()
}
