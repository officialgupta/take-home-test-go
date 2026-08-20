package worker

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v5"
)

const maxProviderAttempts = 3

func withRetry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 200 * time.Millisecond
	return backoff.Retry(ctx, fn, backoff.WithMaxTries(maxProviderAttempts), backoff.WithBackOff(bo))
}
