package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrOrderAlreadyBound = errors.New("order is already bound to a different chain event")
	ErrChainEventBound   = errors.New("chain event is already bound to a different order")
	ErrOrderStateChanged = errors.New("order state changed before chain event binding")
	ErrMissingChainEvent = errors.New("order has no canonical chain event")
)

type ChainEvent struct {
	Id
	EventID    string  `gorm:"column:event_id;type:varchar(255);not null;uniqueIndex" json:"event_id"`
	Network    Network `gorm:"column:network;type:varchar(32);not null;uniqueIndex:idx_chain_event_identity,priority:1" json:"network"`
	TxHash     string  `gorm:"column:tx_hash;type:varchar(128);not null;uniqueIndex:idx_chain_event_identity,priority:2" json:"tx_hash"`
	EventIndex int64   `gorm:"column:event_index;not null;uniqueIndex:idx_chain_event_identity,priority:3" json:"event_index"`
	OrderID    int64   `gorm:"column:order_id;not null;uniqueIndex" json:"order_id"`
	BlockNum   int     `gorm:"column:block_num;not null" json:"block_num"`
	CreatedAt  time.Time
}

func (ChainEvent) TableName() string { return "bep_chain_event" }

func CanonicalEventID(network Network, txHash string, eventIndex int64) string {
	return fmt.Sprintf("%s:%s:%d", strings.ToLower(strings.TrimSpace(string(network))), strings.ToLower(strings.TrimSpace(txHash)), eventIndex)
}

type ChainEventInput struct {
	Network     Network
	TxHash      string
	EventIndex  int64
	BlockNum    int
	FromAddress string
	Timestamp   time.Time
	Amount      decimal.Decimal
}

func BindOrderChainEvent(orderID int64, in ChainEventInput) (Order, ChainEvent, error) {
	var bound Order
	var event ChainEvent
	err := Db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&bound, orderID).Error; err != nil {
			return err
		}
		event = ChainEvent{EventID: CanonicalEventID(in.Network, in.TxHash, in.EventIndex), Network: in.Network,
			TxHash: strings.ToLower(strings.TrimSpace(in.TxHash)), EventIndex: in.EventIndex, OrderID: orderID, BlockNum: in.BlockNum}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&event)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			var existing ChainEvent
			if err := tx.Where("network = ? AND tx_hash = ? AND event_index = ?", event.Network, event.TxHash, event.EventIndex).First(&existing).Error; err == nil {
				if existing.OrderID != orderID {
					return ErrChainEventBound
				}
				event = existing
				return tx.First(&bound, orderID).Error
			}
			return ErrOrderAlreadyBound
		}
		updates := map[string]any{"from_address": in.FromAddress, "confirmed_at": in.Timestamp, "ref_hash": in.TxHash,
			"ref_block_num": in.BlockNum, "status": OrderStatusConfirming}
		if bound.AddressLocked {
			rate, err := decimal.NewFromString(bound.Rate)
			if err != nil {
				return err
			}
			updates["amount"] = in.Amount.String()
			updates["money"] = rate.Mul(in.Amount).String()
		}
		updated := tx.Model(&Order{}).Where("id = ? AND status IN ?", orderID, []int{OrderStatusWaiting, OrderStatusExpired}).Updates(updates)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrOrderStateChanged
		}
		return tx.First(&bound, orderID).Error
	})
	return bound, event, err
}

func FinalizeOrderAndEnqueue(orderID int64) (Order, error) {
	var order Order
	err := Db.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&order, orderID).Error; err != nil {
			return err
		}
		var event ChainEvent
		if err := tx.Where("order_id = ?", orderID).First(&event).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMissingChainEvent
			}
			return err
		}
		updated := tx.Model(&Order{}).Where("id = ? AND status = ?", orderID, OrderStatusConfirming).Update("status", OrderStatusSuccess)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 && order.Status != OrderStatusSuccess {
			return ErrOrderStateChanged
		}
		n := NotificationDelivery{EventID: event.EventID, Kind: NotificationKindOrderSuccess, OrderID: orderID,
			Status: NotificationStatusPending}
		dbNow, err := databaseNow(tx)
		if err != nil {
			return err
		}
		n.NextAttemptAt = dbNow
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&n).Error; err != nil {
			return err
		}
		return tx.First(&order, orderID).Error
	})
	return order, err
}
