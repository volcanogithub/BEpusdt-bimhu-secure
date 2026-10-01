package notify

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/v03413/bepusdt/app/model"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestB1RNotifyFaultHelperProcess(t *testing.T) {
	stage := os.Getenv("B1R_NOTIFY_HELPER_STAGE")
	if stage == "" {
		return
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("BEPUSDT_TEST_POSTGRES_DSN")), &gorm.Config{})
	if err != nil {
		os.Exit(30)
	}
	model.Db = db
	model.RefreshC()
	marker := os.Getenv("B1R_NOTIFY_MARKER")
	worker := "notify-helper-" + stage
	lease := 150 * time.Millisecond
	if stage == "http_before" {
		if _, err := model.ClaimNotification(worker, time.Now(), lease); err != nil {
			os.Exit(31)
		}
		_ = os.WriteFile(marker, []byte(stage), 0o600)
		time.Sleep(time.Hour)
	}
	if err := ProcessOne(t.Context(), worker, callbackTestClient(&http.Client{Timeout: 30 * time.Second}), time.Now(), lease); err != nil {
		os.Exit(32)
	}
	_ = os.WriteFile(marker, []byte(stage), 0o600)
	time.Sleep(time.Hour)
}

type receiverEvent struct {
	EventID string `gorm:"primaryKey;column:event_id"`
}

func (receiverEvent) TableName() string { return "b1r_receiver_event" }

func b1rNotifyPostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("BEPUSDT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("BEPUSDT_TEST_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(32)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`DROP TABLE IF EXISTS b1r_receiver_event, bep_notification_delivery, bep_chain_event, bep_order, bep_conf CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Order{}, &model.ChainEvent{}, &model.NotificationDelivery{}, &model.Conf{}, &receiverEvent{}); err != nil {
		t.Fatal(err)
	}
	model.Db = db
	db.Create(&model.Conf{K: model.ApiAuthToken, V: "b1r-token"})
	model.RefreshC()
	return db
}

func queueB1RPostgresNotification(t *testing.T, db *gorm.DB, label, url string) model.NotificationDelivery {
	t.Helper()
	now := time.Now()
	o := model.Order{OrderId: label, TradeId: "trade-" + label, TradeType: model.UsdtTrc20, Fiat: model.CNY, Crypto: model.USDT,
		Rate: "7", Amount: "1", Money: "7", Address: "TReceiver", Status: model.OrderStatusSuccess, ApiType: model.OrderApiTypeEpusdt,
		NotifyUrl: url, RefHash: "tx-" + label, ExpiredAt: now.Add(time.Hour), ConfirmedAt: &now,
		AutoTimeAt: model.AutoTimeAt{CreatedAt: (*model.Datetime)(&now), UpdatedAt: (*model.Datetime)(&now)}}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	n := model.NotificationDelivery{EventID: "tron:tx-" + label + ":0", Kind: model.NotificationKindOrderSuccess, OrderID: o.ID,
		Status: model.NotificationStatusPending, NextAttemptAt: now.Add(-time.Second)}
	if err := db.Create(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func startNotifyHelper(t *testing.T, stage, marker string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestB1RNotifyFaultHelperProcess$")
	cmd.Env = append(os.Environ(), "B1R_NOTIFY_HELPER_STAGE="+stage, "B1R_NOTIFY_MARKER="+marker)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func waitMarker(t *testing.T, cmd *exec.Cmd, marker string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			t.Fatal("notify helper did not reach fault point")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func killHelper(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()
}

func TestB1RPostgresHTTPProcessKillAndReceiverDedupe(t *testing.T) {
	db := b1rNotifyPostgresDB(t)
	var calls atomic.Int32
	var blockFirst atomic.Bool
	var releaseMu sync.Mutex
	release := make(chan struct{})
	marker := ""
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eventID := r.Header.Get("Idempotency-Key")
		if eventID == "" {
			t.Error("missing stable event id")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls.Add(1)
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&receiverEvent{EventID: eventID}).Error; err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if blockFirst.CompareAndSwap(true, false) {
			_ = os.WriteFile(marker, []byte("receiver committed before response"), 0o600)
			releaseMu.Lock()
			ch := release
			releaseMu.Unlock()
			<-ch
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Kill after claim but before HTTP. The expired lease must be recovered and
	// the durable pending notification must still be delivered.
	before := queueB1RPostgresNotification(t, db, "http-before", callbackTestURL(server.URL))
	marker = filepath.Join(t.TempDir(), "http-before.ready")
	cmd := startNotifyHelper(t, "http_before", marker)
	waitMarker(t, cmd, marker)
	killHelper(t, cmd)
	time.Sleep(250 * time.Millisecond)
	if err := ProcessOne(t.Context(), "restart-before", callbackTestClient(server.Client()), time.Now().Add(24*time.Hour), 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var beforeRow model.NotificationDelivery
	db.First(&beforeRow, before.ID)
	if beforeRow.Status != model.NotificationStatusDelivered {
		t.Fatalf("before-send recovery status=%s", beforeRow.Status)
	}

	// Kill after the receiver atomically records the event but before it sends
	// the response. Retry is required and receiver-side unique event_id dedupes it.
	duringCalls := calls.Load()
	during := queueB1RPostgresNotification(t, db, "http-during", callbackTestURL(server.URL))
	marker = filepath.Join(t.TempDir(), "http-during.ready")
	blockFirst.Store(true)
	cmd = startNotifyHelper(t, "http_during", marker)
	waitMarker(t, cmd, marker)
	killHelper(t, cmd)
	releaseMu.Lock()
	close(release)
	release = make(chan struct{})
	releaseMu.Unlock()
	time.Sleep(250 * time.Millisecond)
	if err := ProcessOne(t.Context(), "restart-during", callbackTestClient(server.Client()), time.Now().Add(-24*time.Hour), 150*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var receiverCount int64
	db.Model(&receiverEvent{}).Where("event_id = ?", during.EventID).Count(&receiverCount)
	if receiverCount != 1 {
		t.Fatalf("receiver business effects=%d, want 1", receiverCount)
	}
	if calls.Load() != duringCalls+2 {
		t.Fatalf("ambiguous HTTP outcome must retry once: before=%d after=%d", duringCalls, calls.Load())
	}
	var duringRow model.NotificationDelivery
	db.First(&duringRow, during.ID)
	if duringRow.Status != model.NotificationStatusDelivered {
		t.Fatalf("during-send recovery status=%s", duringRow.Status)
	}

	// Kill after HTTP success and the delivered update. Restart must not send it.
	afterCalls := calls.Load()
	after := queueB1RPostgresNotification(t, db, "http-after", callbackTestURL(server.URL))
	marker = filepath.Join(t.TempDir(), "http-after.ready")
	cmd = startNotifyHelper(t, "http_after", marker)
	waitMarker(t, cmd, marker)
	killHelper(t, cmd)
	if err := ProcessOne(t.Context(), "restart-after", callbackTestClient(server.Client()), time.Now(), 150*time.Millisecond); !errors.Is(err, model.ErrNoNotificationDue) {
		t.Fatalf("delivered notification was reclaimed: %v", err)
	}
	if calls.Load() != afterCalls+1 {
		t.Fatalf("post-success restart caused extra HTTP calls: before=%d after=%d", afterCalls, calls.Load())
	}
	var afterRow model.NotificationDelivery
	db.First(&afterRow, after.ID)
	if afterRow.Status != model.NotificationStatusDelivered {
		t.Fatalf("after-send persisted status=%s", afterRow.Status)
	}

	// Exactly-once HTTP is intentionally not asserted: the during-send case made
	// two HTTP attempts, while the receiver's atomic event_id key caused one effect.
}
