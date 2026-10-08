package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"newsbot/internal/newsbot"
)

func run(ctx context.Context) error {
	if len(os.Args) < 2 {
		slog.Info("usage: newsbot api|worker|collector|collect|migrate")
		return nil
	}
	cfg, err := newsbot.LoadConfig()
	if err != nil {
		return err
	}
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	store, err := newsbot.NewStore(dbCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer store.DB.Close()
	if os.Args[1] == "migrate" {
		return store.Migrate(ctx, "migrations")
	}
	if os.Args[1] == "collect" || os.Args[1] == "collector" {
		collector := newsbot.Collector{Store: store, Client: newsbot.NewFeedClient()}
		if os.Args[1] == "collect" {
			return collector.Collect(ctx)
		}
		ticker := time.NewTicker(cfg.CollectInterval)
		defer ticker.Stop()
		for {
			if err := collector.Collect(ctx); err != nil {
				slog.Error("collector", "error", err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
			}
		}
	}
	ro, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return err
	}
	ro.DialTimeout = 2 * time.Second
	ro.ReadTimeout = 7 * time.Second
	ro.MaxRetries = 1
	rc := redis.NewClient(ro)
	defer rc.Close()
	switch os.Args[1] {
	case "api":
		api := newsbot.API{Store: store, Redis: rc, Config: cfg}
		handler, err := api.Handler()
		if err != nil {
			return err
		}
		server := newsbot.HTTPServer(cfg.HTTPAddr, handler)
		go func() {
			<-ctx.Done()
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(stopCtx)
		}()
		slog.Info("API listening", "address", cfg.HTTPAddr)
		err = server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case "worker":
		if err := cfg.ValidateAI(); err != nil {
			return err
		}
		if err = os.MkdirAll(cfg.OutputDir, 0750); err != nil {
			return err
		}
		ai := newsbot.NewAI(cfg)
		worker := newsbot.Worker{Store: store, Redis: rc, AI: ai, Renderer: &newsbot.Renderer{OutputDir: cfg.OutputDir, Speaker: ai}}
		return worker.Run(ctx)
	default:
		return errors.New("command không hợp lệ; dùng api|worker|collector|collect|migrate")
	}
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}
