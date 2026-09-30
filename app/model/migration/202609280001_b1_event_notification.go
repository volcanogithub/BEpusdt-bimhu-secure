package migration

import (
	"fmt"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

func m202609280001B1EventAndNotificationConstraints() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202609280001_b1_event_notification_constraints",
		Migrate: func(tx *gorm.DB) error {
			required := []struct{ table, index string }{
				{"bep_chain_event", "idx_chain_event_identity"},
				{"bep_chain_event", "idx_bep_chain_event_order_id"},
				{"bep_notification_delivery", "idx_notification_event_kind"},
			}
			for _, item := range required {
				if !tx.Migrator().HasTable(item.table) || !tx.Migrator().HasIndex(item.table, item.index) {
					return fmt.Errorf("B1 migration invariant missing: %s.%s", item.table, item.index)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			return fmt.Errorf("B1 event/notification migration is intentionally irreversible")
		},
	}
}
