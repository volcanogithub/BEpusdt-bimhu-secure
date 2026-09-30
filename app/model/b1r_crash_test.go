package model

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestB1RFaultHelperProcess(t *testing.T) {
	stage := os.Getenv("B1R_HELPER_STAGE")
	if stage == "" {
		return
	}
	db, err := gorm.Open(postgres.Open(os.Getenv("BEPUSDT_TEST_POSTGRES_DSN")), &gorm.Config{})
	if err != nil {
		os.Exit(20)
	}
	Db = db
	orderID, _ := strconv.ParseInt(os.Getenv("B1R_ORDER_ID"), 10, 64)
	marker := os.Getenv("B1R_MARKER")
	markAndBlock := func() {
		_ = os.WriteFile(marker, []byte(stage), 0o600)
		time.Sleep(time.Hour)
	}

	switch stage {
	case "bind_before":
		markAndBlock()
	case "bind_mid":
		_ = db.Callback().Create().After("gorm:create").Register("b1r:bind-mid", func(tx *gorm.DB) {
			if tx.Statement.Table == "bep_chain_event" {
				markAndBlock()
			}
		})
		_, _, _ = BindOrderChainEvent(orderID, bindInput("crash-bind-mid", 0))
	case "bind_after":
		_, _, err = BindOrderChainEvent(orderID, bindInput("crash-bind-after", 0))
		if err != nil {
			os.Exit(21)
		}
		markAndBlock()
	case "finalize_before":
		markAndBlock()
	case "finalize_mid":
		_ = db.Callback().Create().After("gorm:create").Register("b1r:finalize-mid", func(tx *gorm.DB) {
			if tx.Statement.Table == "bep_notification_delivery" {
				markAndBlock()
			}
		})
		_, _ = FinalizeOrderAndEnqueue(orderID)
	case "finalize_after":
		_, err = FinalizeOrderAndEnqueue(orderID)
		if err != nil {
			os.Exit(22)
		}
		markAndBlock()
	default:
		os.Exit(23)
	}
}

func runAndKillB1RHelper(t *testing.T, stage string, orderID int64) {
	t.Helper()
	marker := filepath.Join(t.TempDir(), stage+".ready")
	cmd := exec.Command(os.Args[0], "-test.run=^TestB1RFaultHelperProcess$")
	cmd.Env = append(os.Environ(), "B1R_HELPER_STAGE="+stage, "B1R_ORDER_ID="+strconv.FormatInt(orderID, 10), "B1R_MARKER="+marker)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			t.Fatalf("helper %s did not reach injection point", stage)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_, _ = cmd.Process.Wait()
}

func TestB1RPostgresProcessKillTransactionBoundaries(t *testing.T) {
	db := b1rPostgresDB(t)
	if err := db.AutoMigrate(&Order{}, &ChainEvent{}, &NotificationDelivery{}); err != nil {
		t.Fatal(err)
	}
	newOrder := func(label string) Order {
		o := b1Order(label)
		if err := db.Create(&o).Error; err != nil {
			t.Fatal(err)
		}
		return o
	}
	assertCounts := func(orderID int64, events, notifications int64) {
		t.Helper()
		var eventCount, notificationCount int64
		db.Model(&ChainEvent{}).Where("order_id = ?", orderID).Count(&eventCount)
		db.Model(&NotificationDelivery{}).Where("order_id = ?", orderID).Count(&notificationCount)
		if eventCount != events || notificationCount != notifications {
			t.Fatalf("order %d event/outbox=%d/%d, want %d/%d", orderID, eventCount, notificationCount, events, notifications)
		}
	}

	before := newOrder("kill-bind-before")
	runAndKillB1RHelper(t, "bind_before", before.ID)
	assertCounts(before.ID, 0, 0)
	if _, _, err := BindOrderChainEvent(before.ID, bindInput("restart-bind-before", 0)); err != nil {
		t.Fatal(err)
	}
	assertCounts(before.ID, 1, 0)

	mid := newOrder("kill-bind-mid")
	runAndKillB1RHelper(t, "bind_mid", mid.ID)
	assertCounts(mid.ID, 0, 0)
	if _, _, err := BindOrderChainEvent(mid.ID, bindInput("crash-bind-mid", 0)); err != nil {
		t.Fatal(err)
	}
	assertCounts(mid.ID, 1, 0)

	after := newOrder("kill-bind-after")
	runAndKillB1RHelper(t, "bind_after", after.ID)
	assertCounts(after.ID, 1, 0)
	if _, _, err := BindOrderChainEvent(after.ID, bindInput("crash-bind-after", 0)); err != nil {
		t.Fatalf("committed binding was not idempotent after restart: %v", err)
	}
	assertCounts(after.ID, 1, 0)

	finalizeBefore := newOrder("kill-finalize-before")
	if _, _, err := BindOrderChainEvent(finalizeBefore.ID, bindInput("finalize-before", 0)); err != nil {
		t.Fatal(err)
	}
	runAndKillB1RHelper(t, "finalize_before", finalizeBefore.ID)
	assertCounts(finalizeBefore.ID, 1, 0)
	if _, err := FinalizeOrderAndEnqueue(finalizeBefore.ID); err != nil {
		t.Fatal(err)
	}
	assertCounts(finalizeBefore.ID, 1, 1)

	finalizeMid := newOrder("kill-finalize-mid")
	if _, _, err := BindOrderChainEvent(finalizeMid.ID, bindInput("finalize-mid", 0)); err != nil {
		t.Fatal(err)
	}
	runAndKillB1RHelper(t, "finalize_mid", finalizeMid.ID)
	var midOrder Order
	db.First(&midOrder, finalizeMid.ID)
	if midOrder.Status != OrderStatusConfirming {
		t.Fatalf("mid-transaction kill leaked success status %d", midOrder.Status)
	}
	assertCounts(finalizeMid.ID, 1, 0)
	if _, err := FinalizeOrderAndEnqueue(finalizeMid.ID); err != nil {
		t.Fatal(err)
	}
	assertCounts(finalizeMid.ID, 1, 1)

	finalizeAfter := newOrder("kill-finalize-after")
	if _, _, err := BindOrderChainEvent(finalizeAfter.ID, bindInput("finalize-after", 0)); err != nil {
		t.Fatal(err)
	}
	runAndKillB1RHelper(t, "finalize_after", finalizeAfter.ID)
	assertCounts(finalizeAfter.ID, 1, 1)
	if _, err := FinalizeOrderAndEnqueue(finalizeAfter.ID); err != nil {
		t.Fatalf("committed finalize was not idempotent: %v", err)
	}
	assertCounts(finalizeAfter.ID, 1, 1)

	if t.Failed() {
		t.Log(fmt.Sprintf("postgres process-kill database retained at DSN %q", os.Getenv("BEPUSDT_TEST_POSTGRES_DSN")))
	}
}
