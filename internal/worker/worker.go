package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"take-home-test-go/internal/providers"
	"take-home-test-go/internal/store"
)

type Config struct {
	PollInterval time.Duration
	BatchSize    int32
	MaxAttempts  int32
	StaleAfter   time.Duration
}

const (
	defaultPollInterval = 5 * time.Second
	defaultBatchSize    = 10
	defaultMaxAttempts  = 5
	defaultStaleAfter   = 2 * time.Minute
)

type Worker struct {
	store  store.Querier
	logger *slog.Logger
	cfg    Config

	geocode   func(postcode string) (providers.GeoResult, error)
	sendEmail func(providers.EmailParams) error
}

func New(s store.Querier, logger *slog.Logger, cfg Config) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultMaxAttempts
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = defaultStaleAfter
	}
	return &Worker{
		store:     s,
		logger:    logger,
		cfg:       cfg,
		geocode:   providers.LookupPostcode,
		sendEmail: providers.SendEmail,
	}
}

// Run polls until ctx is canceled. It always completes whatever row it's
// currently processing before honoring cancellation — a poll tick is never
// aborted mid-row.
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()

	w.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	// Sweep rows orphaned by a worker crash mid-row before claiming new
	// work, so they don't stay stuck in 'processing' forever.
	staleInterval := pgtype.Interval{Microseconds: w.cfg.StaleAfter.Microseconds(), Valid: true}
	if n, err := w.store.ReclaimStaleProcessing(ctx, staleInterval); err != nil {
		w.logger.Error("reclaim stale processing rows failed", "error", err)
	} else if n > 0 {
		w.logger.Warn("reclaimed stale processing rows", "count", n)
	}

	rows, err := w.store.ClaimPendingIngestedForms(ctx, store.ClaimPendingIngestedFormsParams{
		MaxAttempts: w.cfg.MaxAttempts,
		BatchSize:   w.cfg.BatchSize,
	})
	if err != nil {
		w.logger.Error("claim pending ingested forms failed", "error", err)
		return
	}

	for _, row := range rows {
		if ctx.Err() != nil {
			// Shutting down: stop starting new rows, but the current tick's
			// already-claimed rows are left in 'processing' for the next
			// process (or this one, on restart) to reclaim once stale.
			return
		}
		w.process(ctx, row)
	}
}
