package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/v03413/bepusdt/app/model"
	"gorm.io/gorm"
)

func outboxDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "outbox.db")+"?cache=shared&mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(8000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Order{}, &model.ChainEvent{}, &model.NotificationDelivery{}, &model.Conf{}); err != nil {
		t.Fatal(err)
	}
	model.Db = db
	db.Create(&model.Conf{K: model.ApiAuthToken, V: "test-token"})
	model.RefreshC()
	return db
}

func queuedOrder(t *testing.T, db *gorm.DB, url string) model.NotificationDelivery {
	now := time.Now()
	confirmed := now
	o := model.Order{OrderId: "merchant", TradeId: "trade", TradeType: model.UsdtTrc20, Fiat: model.CNY, Crypto: model.USDT,
		Rate: "7", Amount: "1", Money: "7", Address: "TReceiver", Status: model.OrderStatusSuccess, ApiType: model.OrderApiTypeEpusdt,
		NotifyUrl: url, RefHash: "tx", ExpiredAt: now.Add(time.Hour), ConfirmedAt: &confirmed,
		AutoTimeAt: model.AutoTimeAt{CreatedAt: (*model.Datetime)(&now), UpdatedAt: (*model.Datetime)(&now)}}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	n := model.NotificationDelivery{EventID: "tron:tx:0", Kind: model.NotificationKindOrderSuccess, OrderID: o.ID,
		Status: model.NotificationStatusPending, NextAttemptAt: now.Add(-time.Second)}
	if err := db.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestB1ConcurrentWorkersSendOnceAtATime(t *testing.T) {
	db := outboxDB(t)
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Idempotency-Key") != "tron:tx:0" {
			t.Errorf("missing stable idempotency key")
		}
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	queuedOrder(t, db, callbackTestURL(server.URL))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, worker := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(worker string) {
			defer wg.Done()
			<-start
			err := ProcessOne(context.Background(), worker, callbackTestClient(server.Client()), time.Now(), time.Second)
			if err != nil && !errors.Is(err, model.ErrNoNotificationDue) {
				t.Errorf("worker: %v", err)
			}
		}(worker)
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("want one in-flight delivery, got %d", calls.Load())
	}
}

func TestB1TimeoutRetriesWithStableEventID(t *testing.T) {
	db := outboxDB(t)
	var idsMu sync.Mutex
	var ids []string
	slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idsMu.Lock()
		ids = append(ids, r.Header.Get("Idempotency-Key"))
		idsMu.Unlock()
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer slow.Close()
	n := queuedOrder(t, db, callbackTestURL(slow.URL))
	client := callbackTestClient(&http.Client{Timeout: 30 * time.Millisecond})
	if err := ProcessOne(context.Background(), "worker-a", client, time.Now(), time.Second); err == nil {
		t.Fatal("expected timeout")
	}
	var retry model.NotificationDelivery
	db.First(&retry, n.ID)
	if retry.Status != model.NotificationStatusRetry {
		t.Fatalf("want retry, got %s", retry.Status)
	}
	fast := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idsMu.Lock()
		ids = append(ids, r.Header.Get("Idempotency-Key"))
		idsMu.Unlock()
		w.WriteHeader(200)
	}))
	defer fast.Close()
	db.Model(&model.Order{}).Where("id = ?", retry.OrderID).Update("notify_url", callbackTestURL(fast.URL))
	db.Model(&model.NotificationDelivery{}).Where("id = ?", n.ID).Update("next_attempt_at", time.Now().Add(-time.Second))
	if err := ProcessOne(context.Background(), "worker-b", callbackTestClient(fast.Client()), time.Now(), time.Second); err != nil {
		t.Fatal(err)
	}
	idsMu.Lock()
	defer idsMu.Unlock()
	if len(ids) != 2 || ids[0] != ids[1] || ids[0] != "tron:tx:0" {
		t.Fatalf("unstable retry ids: %#v", ids)
	}
}

func TestB1ExpiredLeaseIsRecoveredAfterRestart(t *testing.T) {
	db := outboxDB(t)
	n := queuedOrder(t, db, "http://127.0.0.1")
	claimed, err := model.ClaimNotification("dead-worker", time.Now(), 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != n.ID {
		t.Fatalf("wrong claim")
	}
	time.Sleep(40 * time.Millisecond)
	recovered, err := model.ClaimNotification("restarted-worker", time.Now().Add(time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != n.ID || recovered.LeaseOwner != "restarted-worker" {
		t.Fatalf("not recovered: %#v", recovered)
	}
}

func TestB1RWorkerClockSkewCannotStealLease(t *testing.T) {
	db := outboxDB(t)
	queuedOrder(t, db, "http://127.0.0.1")
	if _, err := model.ClaimNotification("worker-a", time.Now().Add(-24*time.Hour), 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err := model.ClaimNotification("worker-b", time.Now().Add(24*time.Hour), time.Second); !errors.Is(err, model.ErrNoNotificationDue) {
		t.Fatalf("skewed future worker stole live lease: %v", err)
	}
}

func TestB1RSlowHTTPBeyondLeaseIsRenewed(t *testing.T) {
	db := outboxDB(t)
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := maxInFlight.Load()
			if current <= old || maxInFlight.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	queuedOrder(t, db, callbackTestURL(server.URL))

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- ProcessOne(context.Background(), "slow-worker", callbackTestClient(server.Client()), time.Now().Add(-time.Hour), 75*time.Millisecond)
	}()
	time.Sleep(150 * time.Millisecond)
	err := ProcessOne(context.Background(), "competing-worker", callbackTestClient(server.Client()), time.Now().Add(time.Hour), 75*time.Millisecond)
	if err != nil && !errors.Is(err, model.ErrNoNotificationDue) {
		t.Fatal(err)
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || maxInFlight.Load() != 1 {
		t.Fatalf("slow delivery was sent concurrently: calls=%d max_in_flight=%d", calls.Load(), maxInFlight.Load())
	}
}

func TestB1RetryBudgetStopsFurtherClaims(t *testing.T) {
	db := outboxDB(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer server.Close()
	n := queuedOrder(t, db, callbackTestURL(server.URL))
	db.Model(&model.NotificationDelivery{}).Where("id = ?", n.ID).Update("attempt_count", 9)
	if err := ProcessOne(context.Background(), "worker", callbackTestClient(server.Client()), time.Now(), time.Second); err == nil {
		t.Fatal("expected delivery failure")
	}
	var got model.NotificationDelivery
	db.First(&got, n.ID)
	if got.Status != model.NotificationStatusDead || got.AttemptCount != 10 {
		t.Fatalf("retry budget not enforced: %#v", got)
	}
	if _, err := model.ClaimNotification("other", time.Now().Add(time.Hour), time.Second); !errors.Is(err, model.ErrNoNotificationDue) {
		t.Fatalf("dead delivery was reclaimed: %v", err)
	}
}
