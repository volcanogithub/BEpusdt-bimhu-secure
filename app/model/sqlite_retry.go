package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	sqliteBusyAttempts = 16
	sqliteRetryBudget = 5 * time.Second
)

// SQLite permits one writer. Admission is per connection pool, not a global
// application lock: independent pools/processes still contend through SQLite.
var sqliteWriteGates sync.Map // map[*sql.DB]chan struct{}

func sqliteWriteGate(pool *sql.DB) chan struct{} {
	gate := make(chan struct{}, 1)
	actual, _ := sqliteWriteGates.LoadOrStore(pool, gate)
	return actual.(chan struct{})
}

// Inspect the driver's numeric code, never error message text. LOCKED (6)
// alone can mean a programming/cursor error; only LOCKED_SHAREDCACHE (262)
// is retryable. BUSY extended codes include BUSY_SNAPSHOT (517).
func isSQLiteContention(err error) bool {
	var coded interface{ Code() int }
	if !errors.As(err, &coded) {
		return false
	}
	code := coded.Code()
	return code&0xff == 5 || code == 262
}

// Only replay database-only operations after their transaction rolled back.
// Do not put HTTP, MQTT, credentials, or other external effects in operation.
// PostgreSQL executes exactly once, without a retry or context-policy change.
func databaseWrite(db *gorm.DB, operation func(*gorm.DB) error) error {
	if db.Dialector.Name() != "sqlite" {
		return operation(db)
	}
	parent := db.Statement.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, sqliteRetryBudget)
	defer cancel()
	pool, err := db.DB()
	if err != nil {
		return err
	}
	gate := sqliteWriteGate(pool)
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	var last error
	for attempt := 0; attempt < sqliteBusyAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return errors.Join(err, last)
		}
		last = operation(db.WithContext(ctx))
		if !isSQLiteContention(last) {
			return last
		}
		if attempt+1 == sqliteBusyAttempts {
			break
		}
		// Capped exponential backoff plus jitter avoids synchronized workers.
		delay := time.Millisecond << min(attempt, 4)
		delay += time.Duration(rand.Int64N(int64(delay)))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(ctx.Err(), last)
		case <-timer.C:
		}
	}
	return fmt.Errorf("SQLite contention retry exhausted after %d attempts: %w", sqliteBusyAttempts, last)
}

func databaseTransaction(db *gorm.DB, operation func(*gorm.DB) error) error {
	return databaseWrite(db, func(attempt *gorm.DB) error {
		// A new transaction/snapshot on EVERY attempt; never retry one UPDATE
		// inside a transaction whose read snapshot is already obsolete.
		return attempt.Transaction(operation)
	})
}
