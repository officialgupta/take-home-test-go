package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"take-home-test-go/internal/app"
	"take-home-test-go/internal/store"
	"take-home-test-go/internal/worker"
)

func main() {
	cfg := loadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	logger := slog.Default()
	q := store.New(pool)

	w := worker.New(q, logger, worker.Config{
		PollInterval: cfg.WorkerPollInterval,
		BatchSize:    cfg.WorkerBatchSize,
		MaxAttempts:  cfg.WorkerMaxAttempts,
		StaleAfter:   cfg.WorkerStaleAfter,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           app.New(q, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown error", "error", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := w.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("worker stopped with error", "error", err)
		}
	}()

	logger.Info("server starting", "port", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}

	wg.Wait()
	logger.Info("server stopped")
}

type Config struct {
	Port               string
	DatabaseURL        string
	WorkerPollInterval time.Duration
	WorkerBatchSize    int32
	WorkerMaxAttempts  int32
	WorkerStaleAfter   time.Duration
}

func loadConfig() Config {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	return Config{
		Port:               getEnvOr("PORT", "3000"),
		DatabaseURL:        databaseURL,
		WorkerPollInterval: getEnv("POLL_INTERVAL", 5*time.Second, time.ParseDuration),
		WorkerBatchSize:    getEnv("WORKER_BATCH_SIZE", int32(10), parseInt32),
		WorkerMaxAttempts:  getEnv("WORKER_MAX_ATTEMPTS", int32(5), parseInt32),
		WorkerStaleAfter:   getEnv("WORKER_STALE_AFTER", 2*time.Minute, time.ParseDuration),
	}
}

// getEnvOr returns the value of key, or fallback if it is unset.
func getEnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnv reads key from the environment, parses it with parse, and returns
// fallback if the variable is unset. It exits the process if parsing fails.
func getEnv[T any](key string, fallback T, parse func(string) (T, error)) T {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	parsed, err := parse(v)
	if err != nil {
		log.Fatalf("invalid %s=%q: %v", key, v, err)
	}
	return parsed
}

func parseInt32(s string) (int32, error) {
	n, err := strconv.ParseInt(s, 10, 32)
	return int32(n), err
}
