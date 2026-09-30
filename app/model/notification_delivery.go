package model

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

const (
	NotificationKindOrderSuccess = "order.success"
	NotificationStatusPending    = "pending"
	NotificationStatusLeased     = "leased"
	NotificationStatusRetry      = "retry"
	NotificationStatusDelivered  = "delivered"
	NotificationStatusDead       = "dead"
)

var ErrNoNotificationDue = errors.New("no notification is due")

type NotificationDelivery struct {
	Id
	EventID       string     `gorm:"column:event_id;type:varchar(255);not null;uniqueIndex:idx_notification_event_kind,priority:1" json:"event_id"`
	Kind          string     `gorm:"column:kind;type:varchar(64);not null;uniqueIndex:idx_notification_event_kind,priority:2" json:"kind"`
	OrderID       int64      `gorm:"column:order_id;not null;index" json:"order_id"`
	Status        string     `gorm:"column:status;type:varchar(16);not null;index" json:"status"`
	LeaseOwner    string     `gorm:"column:lease_owner;type:varchar(128);not null;default:''" json:"-"`
	LeaseUntil    *time.Time `gorm:"column:lease_until" json:"-"`
	NextAttemptAt time.Time  `gorm:"column:next_attempt_at;not null;index" json:"next_attempt_at"`
	AttemptCount  int        `gorm:"column:attempt_count;not null;default:0" json:"attempt_count"`
	LastError     string     `gorm:"column:last_error;type:varchar(512);not null;default:''" json:"last_error"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (NotificationDelivery) TableName() string { return "bep_notification_delivery" }

func databaseNow(tx *gorm.DB) (time.Time, error) {
	var seconds float64
	query := "SELECT EXTRACT(EPOCH FROM CURRENT_TIMESTAMP)"
	if tx.Dialector.Name() == "sqlite" {
		query = "SELECT unixepoch('subsec')"
	}
	if err := tx.Raw(query).Scan(&seconds).Error; err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}
	whole := int64(seconds)
	return time.Unix(whole, int64((seconds-float64(whole))*float64(time.Second))), nil
}

// ClaimNotification deliberately ignores the caller clock. Lease eligibility
// and deadlines are derived from the database clock so skewed workers cannot
// steal or indefinitely retain a delivery.
func ClaimNotification(worker string, _ time.Time, lease time.Duration) (NotificationDelivery, error) {
	var claimed NotificationDelivery
	err := Db.Transaction(func(tx *gorm.DB) error {
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		var candidate NotificationDelivery
		err = tx.Where("next_attempt_at <= ? AND (status IN ? OR (status = ? AND lease_until < ?))", now,
			[]string{NotificationStatusPending, NotificationStatusRetry}, NotificationStatusLeased, now).
			Order("next_attempt_at ASC, id ASC").First(&candidate).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNoNotificationDue
			}
			return err
		}
		until := now.Add(lease)
		updated := tx.Model(&NotificationDelivery{}).
			Where("id = ? AND next_attempt_at <= ? AND (status IN ? OR (status = ? AND lease_until < ?))", candidate.ID, now,
				[]string{NotificationStatusPending, NotificationStatusRetry}, NotificationStatusLeased, now).
			Updates(map[string]any{"status": NotificationStatusLeased, "lease_owner": worker, "lease_until": until, "attempt_count": gorm.Expr("attempt_count + 1")})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrNoNotificationDue
		}
		return tx.First(&claimed, candidate.ID).Error
	})
	return claimed, err
}

func ExtendNotificationLease(id int64, worker string, lease time.Duration) error {
	return Db.Transaction(func(tx *gorm.DB) error {
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		r := tx.Model(&NotificationDelivery{}).
			Where("id = ? AND status = ? AND lease_owner = ? AND lease_until >= ?", id, NotificationStatusLeased, worker, now).
			Update("lease_until", now.Add(lease))
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrOrderStateChanged
		}
		return nil
	})
}

func CompleteNotification(id int64, worker string) error {
	r := Db.Model(&NotificationDelivery{}).Where("id = ? AND status = ? AND lease_owner = ?", id, NotificationStatusLeased, worker).
		Updates(map[string]any{"status": NotificationStatusDelivered, "lease_owner": "", "lease_until": nil, "last_error": ""})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrOrderStateChanged
	}
	return nil
}

func RetryNotification(id int64, worker, reason string, next time.Time) error {
	if len(reason) > 512 {
		reason = reason[:512]
	}
	r := Db.Model(&NotificationDelivery{}).Where("id = ? AND status = ? AND lease_owner = ?", id, NotificationStatusLeased, worker).
		Updates(map[string]any{"status": NotificationStatusRetry, "lease_owner": "", "lease_until": nil, "last_error": reason, "next_attempt_at": next})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrOrderStateChanged
	}
	return nil
}

func RetryNotificationAfter(id int64, worker, reason string, delay time.Duration) error {
	if len(reason) > 512 {
		reason = reason[:512]
	}
	return Db.Transaction(func(tx *gorm.DB) error {
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		r := tx.Model(&NotificationDelivery{}).Where("id = ? AND status = ? AND lease_owner = ?", id, NotificationStatusLeased, worker).
			Updates(map[string]any{"status": NotificationStatusRetry, "lease_owner": "", "lease_until": nil, "last_error": reason, "next_attempt_at": now.Add(delay)})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrOrderStateChanged
		}
		return nil
	})
}

func FailNotification(id int64, worker, reason string) error {
	if len(reason) > 512 {
		reason = reason[:512]
	}
	r := Db.Model(&NotificationDelivery{}).Where("id = ? AND status = ? AND lease_owner = ?", id, NotificationStatusLeased, worker).
		Updates(map[string]any{"status": NotificationStatusDead, "lease_owner": "", "lease_until": nil, "last_error": reason})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrOrderStateChanged
	}
	return nil
}

func RequeueOrderNotification(orderID int64, _ time.Time) error {
	return Db.Transaction(func(tx *gorm.DB) error {
		now, err := databaseNow(tx)
		if err != nil {
			return err
		}
		r := tx.Model(&NotificationDelivery{}).Where("order_id = ? AND status <> ?", orderID, NotificationStatusLeased).
			Updates(map[string]any{"status": NotificationStatusPending, "next_attempt_at": now, "last_error": ""})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected != 1 {
			return ErrNoNotificationDue
		}
		return nil
	})
}
