package task

import (
	"context"
	"fmt"
	"github.com/glebarez/sqlite"
	"github.com/v03413/bepusdt/app/model"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestHTTPSObserverExpiredTailSelection(t *testing.T) {
	db, e := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if e != nil {
		t.Fatal(e)
	}
	old := model.Db
	model.Db = db
	t.Cleanup(func() { model.Db = old })
	if e = db.AutoMigrate(&model.Order{}, &model.ChainEvent{}); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)
	created := model.Datetime(now.Add(-time.Hour))
	zero := time.Time{}
	ids := []int64{}
	cases := []struct {
		status  int
		expires time.Time
		addr    string
		bound   bool
		want    bool
	}{
		{model.OrderStatusWaiting, now.Add(time.Hour), httpsObserverRecipient, false, true},
		{model.OrderStatusExpired, now.Add(-time.Minute), httpsObserverRecipient, false, true},
		{model.OrderStatusExpired, cutoff.Add(-time.Minute), httpsObserverRecipient, false, false},
		{model.OrderStatusSuccess, now.Add(time.Hour), httpsObserverRecipient, false, false},
		{model.OrderStatusWaiting, now.Add(time.Hour), "other-public-fixture", false, false},
		{model.OrderStatusWaiting, now.Add(time.Hour), httpsObserverRecipient, true, false},
	}
	for i, c := range cases {
		o := model.Order{OrderId: fmt.Sprint(i), TradeId: fmt.Sprintf("tail-%d", i), TradeType: model.UsdtTrc20, Rate: "1", Amount: "1", Money: "1", Address: c.addr, Status: c.status, ExpiredAt: c.expires, ConfirmedAt: &zero, AutoTimeAt: model.AutoTimeAt{CreatedAt: &created}}
		if e = db.Create(&o).Error; e != nil {
			t.Fatal(e)
		}
		if c.bound {
			if e = db.Exec("INSERT INTO bep_chain_event (event_id,network,tx_hash,event_index,order_id,block_num) VALUES (?,?,?,?,?,?)", fmt.Sprint(i), "tron", fmt.Sprint(i), 0, o.ID, 100).Error; e != nil {
				t.Fatal(e)
			}
		}
		if c.want {
			ids = append(ids, o.ID)
		}
	}
	got, e := httpsObserverOrders(context.Background(), cutoff)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != len(ids) {
		t.Fatalf("selected %d orders, expected %d", len(got), len(ids))
	}
	for i, o := range got {
		if o.ID != ids[i] {
			t.Fatalf("unexpected selection at %d", i)
		}
	}
}
func TestCanonicalOrderWindowExactMilliseconds(t *testing.T) {
	start := time.UnixMilli(1791555000123)
	created := model.Datetime(start)
	o := model.Order{AutoTimeAt: model.AutoTimeAt{CreatedAt: &created}, ExpiredAt: start.Add(time.Minute)}
	for _, c := range []struct {
		at   time.Time
		want bool
	}{{start.Add(-time.Millisecond), false}, {start, true}, {start.Add(time.Millisecond), true}, {o.ExpiredAt.Add(-time.Millisecond), true}, {o.ExpiredAt, false}} {
		if got := withinCanonicalOrderWindow(o, c.at); got != c.want {
			t.Fatalf("window at %d: %v", c.at.UnixMilli(), got)
		}
	}
	o.CreatedAt = nil
	if withinCanonicalOrderWindow(o, start) {
		t.Fatal("missing creation time accepted")
	}
}
