package model

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type r2CodeError int

func (e r2CodeError) Error() string { return fmt.Sprintf("injected database code %d", e) }
func (e r2CodeError) Code() int     { return int(e) }

func r2SQLiteDB(t *testing.T) (*gorm.DB, *gorm.DB) {
	t.Helper()
	// Independent pools/private caches, like two processes, not a single writer mutex.
	dsn := filepath.Join(t.TempDir(), "r2.db") + "?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(50)"
	open := func() *gorm.DB {
		db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		pool.SetMaxOpenConns(8)
		t.Cleanup(func() { _ = pool.Close() })
		return db
	}
	db, other := open(), open()
	if err := db.AutoMigrate(&Order{}, &ChainEvent{}, &NotificationDelivery{}); err != nil {
		t.Fatal(err)
	}
	Db = db
	return db, other
}

func TestB1R2SQLiteSnapshotRetryStartsFreshTransaction(t *testing.T) {
	db, other := r2SQLiteDB(t)
	if err := db.Exec("CREATE TABLE r2_counter (id INTEGER PRIMARY KEY, value INTEGER NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO r2_counter VALUES (1, 1)").Error; err != nil {
		t.Fatal(err)
	}
	attempts := 0
	err := databaseTransaction(db, func(tx *gorm.DB) error {
		attempts++
		var value int
		if err := tx.Raw("SELECT value FROM r2_counter WHERE id = 1").Scan(&value).Error; err != nil {
			return err
		}
		if attempts == 1 {
			// Commit another writer after the read: first UPDATE must encounter
			// BUSY_SNAPSHOT, which busy_timeout alone cannot repair.
			if err := other.Exec("UPDATE r2_counter SET value = 2 WHERE id = 1").Error; err != nil {
				return err
			}
		}
		return tx.Exec("UPDATE r2_counter SET value = ? WHERE id = 1", value+1).Error
	})
	if err != nil || attempts != 2 {
		t.Fatalf("fresh snapshot attempts=%d err=%v", attempts, err)
	}
	var got int
	db.Raw("SELECT value FROM r2_counter WHERE id = 1").Scan(&got)
	if got != 3 {
		t.Fatalf("stale read/double write: value=%d, want 3", got)
	}
}

func TestB1R2SQLiteRetryCodesBudgetAndCancellation(t *testing.T) {
	db, _ := r2SQLiteDB(t)
	for _, code := range []int{5, 261, 517, 773, 262} {
		if !isSQLiteContention(fmt.Errorf("wrapped: %w", r2CodeError(code))) {
			t.Fatalf("contention code %d not recognized", code)
		}
	}
	for _, code := range []int{1, 6, 19, 2067, 10} {
		calls := 0
		err := databaseWrite(db, func(*gorm.DB) error { calls++; return r2CodeError(code) })
		if err == nil || calls != 1 {
			t.Fatalf("real error code %d was retried/hidden: calls=%d err=%v", code, calls, err)
		}
	}
	if isSQLiteContention(errors.New("database is locked (5)")) {
		t.Fatal("message text must not classify an error")
	}
	calls := 0
	err := databaseWrite(db, func(*gorm.DB) error { calls++; return r2CodeError(517) })
	var coded interface{ Code() int }
	if calls != sqliteBusyAttempts || !errors.As(err, &coded) || coded.Code() != 517 {
		t.Fatalf("retry budget/error identity: calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	err = databaseWrite(db.WithContext(ctx), func(*gorm.DB) error { calls++; return r2CodeError(5) })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("cancel ignored: calls=%d err=%v", calls, err)
	}
}

func TestB1R2SQLiteRealErrorsRollbackWithoutRetry(t *testing.T) {
	db, _ := r2SQLiteDB(t)
	if err := db.Exec("CREATE TABLE r2_unique (value TEXT UNIQUE)").Error; err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"MALFORMED SQL", "INSERT INTO r2_unique VALUES ('duplicate'), ('duplicate')"} {
		attempts := 0
		err := databaseTransaction(db, func(tx *gorm.DB) error {
			attempts++
			return tx.Exec(sql).Error
		})
		if err == nil || attempts != 1 || isSQLiteContention(err) {
			t.Fatalf("real SQL error hidden/retried: attempts=%d err=%v", attempts, err)
		}
	}
	var count int64
	db.Table("r2_unique").Count(&count)
	if count != 0 {
		t.Fatal("constraint failure leaked a partial transaction")
	}
}

func TestB1R2SQLiteConcurrentFinalizeClaimAndStateUpdates(t *testing.T) {
	db, other := r2SQLiteDB(t)
	const total = 24
	orders := make([]Order, total)
	for i := range orders {
		orders[i] = b1Order(fmt.Sprintf("r2-%d", i))
		if err := db.Create(&orders[i]).Error; err != nil {
			t.Fatal(err)
		}
		if _, _, err := BindOrderChainEvent(orders[i].ID, bindInput(fmt.Sprintf("r2-tx-%d", i), 0)); err != nil {
			t.Fatal(err)
		}
	}
	var delivered atomic.Int32
	var producers sync.WaitGroup
	var workers sync.WaitGroup
	start := make(chan struct{})
	results := make(chan error, total+9)
	for i := range orders {
		producers.Add(1)
		go func(id int64) {
			defer producers.Done()
			<-start
			_, err := FinalizeOrderAndEnqueue(id)
			results <- err
		}(orders[i].ID)
	}
	producersDone := make(chan struct{})
	go func() { producers.Wait(); close(producersDone) }()
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(worker string) {
			defer workers.Done()
			<-start
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				n, err := ClaimNotification(worker, time.Now(), 5*time.Second)
				if errors.Is(err, ErrNoNotificationDue) {
					select {
					case <-producersDone:
						if delivered.Load() == total {
							return
						}
					default:
					}
					time.Sleep(time.Millisecond)
					continue
				}
				if err != nil {
					results <- err
					return
				}
				if err := ExtendNotificationLease(n.ID, worker, 5*time.Second); err != nil {
					results <- err
					return
				}
				if err := CompleteNotification(n.ID, worker); err != nil {
					results <- err
					return
				}
				delivered.Add(1)
			}
			results <- fmt.Errorf("worker %s did not drain queue", worker)
		}(fmt.Sprintf("r2-worker-%d", i))
	}
	// A second independent pool adds actual competing writes.
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for i := 0; i < total; i++ {
			if err := databaseTransaction(other, func(tx *gorm.DB) error {
				return tx.Model(&Order{}).Where("id = ?", orders[i].ID).Update("money", "7").Error
			}); err != nil {
				results <- err
				return
			}
		}
	}()
	close(start)
	producers.Wait()
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("unrecovered contention: %v", err)
		}
	}
	var successful, outbox, completed int64
	db.Model(&Order{}).Where("status = ?", OrderStatusSuccess).Count(&successful)
	db.Model(&NotificationDelivery{}).Count(&outbox)
	db.Model(&NotificationDelivery{}).Where("status = ?", NotificationStatusDelivered).Count(&completed)
	if successful != total || outbox != total || completed != total || delivered.Load() != total {
		t.Fatalf("success/outbox/delivered/claims=%d/%d/%d/%d", successful, outbox, completed, delivered.Load())
	}
}

func TestB1R2SQLiteBusyTimeoutOnEveryPoolConnection(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "pool.db"), ""); err != nil {
		t.Fatal(err)
	}
	pool, err := Db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for i := 0; i < 3; i++ {
		conn, err := pool.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		var timeout int
		if err := conn.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 8000 {
			t.Fatalf("connection %d timeout=%d err=%v", i, timeout, err)
		}
	}
}

func TestB1R2PostgresRetryWrapperIsPassThrough(t *testing.T) {
	db := b1rPostgresDB(t)
	for _, code := range []int{5, 517, 262, 19} {
		calls := 0
		err := databaseWrite(db, func(*gorm.DB) error { calls++; return r2CodeError(code) })
		if calls != 1 || err == nil {
			t.Fatalf("PostgreSQL behavior changed: code=%d calls=%d err=%v", code, calls, err)
		}
	}
}
