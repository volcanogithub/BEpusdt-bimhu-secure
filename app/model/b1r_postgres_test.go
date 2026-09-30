package model

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func b1rPostgresDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("BEPUSDT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("BEPUSDT_TEST_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(32)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`DROP TABLE IF EXISTS bep_notification_delivery, bep_chain_event, bep_notify_record, bep_order, bep_wallet, bep_conf, bep_rate, bep_migration CASCADE`).Error; err != nil {
		t.Fatal(err)
	}
	Db = db
	return db
}

func TestB1RPostgresAcceptance(t *testing.T) {
	t.Run("empty migration and indexes", func(t *testing.T) {
		db := b1rPostgresDB(t)
		if err := AutoMigrate(); err != nil {
			t.Fatal(err)
		}
		for _, check := range []struct{ table, index string }{
			{"bep_chain_event", "idx_chain_event_identity"},
			{"bep_chain_event", "idx_bep_chain_event_order_id"},
			{"bep_notification_delivery", "idx_notification_event_kind"},
		} {
			if !db.Migrator().HasIndex(check.table, check.index) {
				t.Fatalf("missing %s.%s", check.table, check.index)
			}
		}
	})

	t.Run("historical conflict stops migration", func(t *testing.T) {
		db := b1rPostgresDB(t)
		if err := db.AutoMigrate(&Order{}); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			o := b1Order(fmt.Sprintf("pg-legacy-%d", i))
			o.Status, o.RefHash = OrderStatusConfirming, "DUPLICATE"
			if err := db.Create(&o).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := AutoMigrate(); err == nil {
			t.Fatal("migration accepted ambiguous historical transaction")
		}
		if db.Migrator().HasTable(&ChainEvent{}) {
			t.Fatal("migration created B1 tables after conflict preflight failed")
		}
	})

	t.Run("unique event and conditional order claim", func(t *testing.T) {
		db := b1rPostgresDB(t)
		if err := db.AutoMigrate(&Order{}, &ChainEvent{}, &NotificationDelivery{}); err != nil {
			t.Fatal(err)
		}
		o := b1Order("pg-race")
		if err := db.Create(&o).Error; err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		results := make(chan error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, _, err := BindOrderChainEvent(o.ID, bindInput(fmt.Sprintf("pg-tx-%d", i), int64(i)))
				results <- err
			}(i)
		}
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatalf("successful order claims=%d, want 1", success)
		}
		var count int64
		db.Model(&ChainEvent{}).Where("order_id = ?", o.ID).Count(&count)
		if count != 1 {
			t.Fatalf("durable event bindings=%d, want 1", count)
		}

		o2 := b1Order("pg-second")
		db.Create(&o2)
		var event ChainEvent
		db.Where("order_id = ?", o.ID).First(&event)
		if _, _, err := BindOrderChainEvent(o2.ID, bindInput(event.TxHash, event.EventIndex)); !errors.Is(err, ErrChainEventBound) {
			t.Fatalf("duplicate event error=%v", err)
		}
	})

	t.Run("finalize and outbox rollback together", func(t *testing.T) {
		db := b1rPostgresDB(t)
		if err := db.AutoMigrate(&Order{}, &ChainEvent{}, &NotificationDelivery{}); err != nil {
			t.Fatal(err)
		}
		o := b1Order("pg-rollback")
		db.Create(&o)
		bound, _, err := BindOrderChainEvent(o.ID, bindInput("pg-rollback-tx", 0))
		if err != nil {
			t.Fatal(err)
		}
		name := "b1r:pg-fail-outbox"
		if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
			if tx.Statement.Table == "bep_notification_delivery" {
				tx.AddError(errors.New("injected postgres outbox failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := FinalizeOrderAndEnqueue(bound.ID); err == nil {
			t.Fatal("expected injected failure")
		}
		db.Callback().Create().Remove(name)
		var got Order
		db.First(&got, bound.ID)
		if got.Status != OrderStatusConfirming {
			t.Fatalf("order status=%d after rollback", got.Status)
		}
		var count int64
		db.Model(&NotificationDelivery{}).Count(&count)
		if count != 0 {
			t.Fatalf("outbox rows=%d after rollback", count)
		}
	})

	t.Run("lease competition database clock and recovery", func(t *testing.T) {
		db := b1rPostgresDB(t)
		if err := db.AutoMigrate(&Order{}, &ChainEvent{}, &NotificationDelivery{}); err != nil {
			t.Fatal(err)
		}
		o := b1Order("pg-lease")
		db.Create(&o)
		n := NotificationDelivery{EventID: "tron:pg-lease:0", Kind: NotificationKindOrderSuccess, OrderID: o.ID,
			Status: NotificationStatusPending, NextAttemptAt: time.Now().Add(-time.Hour)}
		db.Create(&n)

		var wg sync.WaitGroup
		claims := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := ClaimNotification(fmt.Sprintf("pg-worker-%d", i), time.Now().Add(time.Duration(i-4)*24*time.Hour), 5*time.Second)
				claims <- err
			}(i)
		}
		wg.Wait()
		close(claims)
		success := 0
		for err := range claims {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatalf("lease claims=%d, want 1", success)
		}
		if _, err := ClaimNotification("future-clock", time.Now().Add(365*24*time.Hour), time.Second); !errors.Is(err, ErrNoNotificationDue) {
			t.Fatalf("future clock stole lease: %v", err)
		}
		if err := db.Exec(`UPDATE bep_notification_delivery SET lease_until = CURRENT_TIMESTAMP - INTERVAL '1 second' WHERE id = ?`, n.ID).Error; err != nil {
			t.Fatal(err)
		}
		recovered, err := ClaimNotification("restarted", time.Now().Add(-365*24*time.Hour), time.Second)
		if err != nil || recovered.ID != n.ID {
			t.Fatalf("expired lease recovery=%#v err=%v", recovered, err)
		}
	})
}
