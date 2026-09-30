package model

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/v03413/bepusdt/app/model/migration"
	"gorm.io/gorm"
)

func b1DB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "b1.db")+"?cache=shared&mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(8000)"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&Order{}, &ChainEvent{}, &NotificationDelivery{}); err != nil {
		t.Fatal(err)
	}
	Db = db
	return db
}

func b1Order(id string) Order {
	now := time.Now().Add(-time.Minute)
	zero := time.Unix(0, 0)
	return Order{OrderId: id, TradeId: "trade-" + id, TradeType: UsdtTrc20, Fiat: CNY, Crypto: USDT,
		Rate: "7", Amount: "1", Money: "7", Address: "TReceiver", MatchAddress: "TReceiver",
		Status: OrderStatusWaiting, ApiType: OrderApiTypeEpusdt, NotifyUrl: "http://127.0.0.1",
		ExpiredAt: now.Add(time.Hour), ConfirmedAt: &zero,
		AutoTimeAt: AutoTimeAt{CreatedAt: (*Datetime)(&now), UpdatedAt: (*Datetime)(&now)}}
}

func bindInput(tx string, index int64) ChainEventInput {
	return ChainEventInput{Network: "tron", TxHash: tx, EventIndex: index, BlockNum: 100,
		FromAddress: "TFrom", Timestamp: time.Now(), Amount: decimal.RequireFromString("1")}
}

func TestB1EventIdentityAllowsDistinctEventsInOneTransaction(t *testing.T) {
	db := b1DB(t)
	o1, o2 := b1Order("one"), b1Order("two")
	if err := db.Create(&o1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&o2).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := BindOrderChainEvent(o1.ID, bindInput("ABC", 0)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := BindOrderChainEvent(o2.ID, bindInput("ABC", 1<<32)); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&ChainEvent{}).Where("tx_hash = ?", "abc").Count(&count)
	if count != 2 {
		t.Fatalf("want two distinct events, got %d", count)
	}
}

func TestB1SameEventCannotBindTwoOrders(t *testing.T) {
	db := b1DB(t)
	o1, o2 := b1Order("one"), b1Order("two")
	db.Create(&o1)
	db.Create(&o2)
	if _, _, err := BindOrderChainEvent(o1.ID, bindInput("same", 7)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := BindOrderChainEvent(o2.ID, bindInput("same", 7)); !errors.Is(err, ErrChainEventBound) {
		t.Fatalf("want ErrChainEventBound, got %v", err)
	}
}

func TestB1ConcurrentEventsCannotOverwriteOrder(t *testing.T) {
	db := b1DB(t)
	o := b1Order("race")
	db.Create(&o)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := BindOrderChainEvent(o.ID, bindInput(fmt.Sprintf("tx-%d", i), int64(i)))
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
		t.Fatalf("exactly one event must claim the order, successes=%d", success)
	}
	var count int64
	db.Model(&ChainEvent{}).Where("order_id = ?", o.ID).Count(&count)
	if count != 1 {
		t.Fatalf("want one durable binding, got %d", count)
	}
}

func TestB1FinalizeRollsBackWhenOutboxWriteFails(t *testing.T) {
	db := b1DB(t)
	o := b1Order("rollback")
	db.Create(&o)
	bound, _, err := BindOrderChainEvent(o.ID, bindInput("rollback-tx", 0))
	if err != nil {
		t.Fatal(err)
	}
	name := "b1:fail-notification-insert"
	if err := db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "bep_notification_delivery" {
			tx.AddError(errors.New("injected outbox failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Create().Remove(name) })
	if _, err := FinalizeOrderAndEnqueue(bound.ID); err == nil {
		t.Fatal("expected injected failure")
	}
	var got Order
	db.First(&got, bound.ID)
	if got.Status != OrderStatusConfirming {
		t.Fatalf("order escaped transaction with status %d", got.Status)
	}
	var count int64
	db.Model(&NotificationDelivery{}).Count(&count)
	if count != 0 {
		t.Fatalf("unexpected outbox rows: %d", count)
	}
}

func TestB1FinalizeCreatesExactlyOneNotification(t *testing.T) {
	db := b1DB(t)
	o := b1Order("finalize")
	db.Create(&o)
	bound, event, err := BindOrderChainEvent(o.ID, bindInput("final-tx", 3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = FinalizeOrderAndEnqueue(bound.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = FinalizeOrderAndEnqueue(bound.ID); err != nil {
		t.Fatal(err)
	}
	var rows []NotificationDelivery
	db.Find(&rows)
	if len(rows) != 1 || rows[0].EventID != event.EventID {
		t.Fatalf("unexpected outbox: %#v", rows)
	}
}

func TestB1MigrationStopsOnHistoricalConflict(t *testing.T) {
	db := b1DB(t)
	o1, o2 := b1Order("legacy-1"), b1Order("legacy-2")
	for _, o := range []*Order{&o1, &o2} {
		o.Status = OrderStatusConfirming
		o.RefHash = "DUPLICATE"
		if err := db.Create(o).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := migration.ValidateB1HistoricalConflicts(db); err == nil {
		t.Fatal("migration must stop on historical conflict")
	}
}

func TestB1ModelInitCreatesRequiredConstraints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "init.db")
	if err := Init(path, ""); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := Db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	checks := []struct{ table, index string }{
		{"bep_chain_event", "idx_chain_event_identity"},
		{"bep_chain_event", "idx_bep_chain_event_order_id"},
		{"bep_notification_delivery", "idx_notification_event_kind"},
	}
	for _, check := range checks {
		if !Db.Migrator().HasIndex(check.table, check.index) {
			t.Fatalf("missing %s.%s", check.table, check.index)
		}
	}
}
