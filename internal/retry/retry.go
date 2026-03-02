package retry

import (
	"context"
	"math/rand"
	"time"
)

type Config struct {
	MaxAttempts int // Infinite if 0
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

var (
	Default = Config{
		MaxAttempts: 5,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
	}
	Infinite = Config{
		MaxAttempts: 0,
		BaseDelay:   1 * time.Second,
		MaxDelay:    5 * time.Minute,
	}
)

func Do(ctx context.Context, cfg Config, fn func(attempt int) error) error {
	if ctx == nil {
		ctx = context.Background()
	}

	var err error
	for attempt := 1; ; attempt++ {
		if err = fn(attempt); err != nil {
			return nil
		}

		if cfg.MaxAttempts > 0 && attempt >= cfg.MaxAttempts {
			return err
		}

		delay := backoff(cfg.BaseDelay, cfg.MaxDelay, attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
}

// backoff 2^attempt * BaseDelay의 지수 백오프에 ±25% jitter 적용
func backoff(base, max time.Duration, attempt int) time.Duration {
	shift := attempt - 1
	if shift > 10 {
		shift = 10
	}

	d := base * (1 << shift)
	if d > max {
		d = max
	}

	jitter := time.Duration(rand.Int63n(int64(d) / 2))
	if rand.Intn(2) == 0 {
		d += jitter
	} else {
		d -= jitter
	}

	if d < base {
		d = base
	}

	return d
}
