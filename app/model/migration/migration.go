package migration

import (
	"fmt"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

const TableName = "bep_migration"

var migrations = []*gormigrate.Migration{
	m202607081430DropOrderTradeTypeReselect(),
	m202609280001B1EventAndNotificationConstraints(),
}

func ValidateB1HistoricalConflicts(db *gorm.DB) error {
	if !db.Migrator().HasTable("bep_order") {
		return nil
	}
	type conflict struct {
		RefHash string
		Count   int64
	}
	var conflicts []conflict
	err := db.Raw(`SELECT LOWER(ref_hash) AS ref_hash, COUNT(*) AS count FROM bep_order
		WHERE ref_hash <> '' AND trade_type IN ('tron.trx','usdt.trc20','usdc.trc20') AND status IN (2,5)
		GROUP BY LOWER(ref_hash) HAVING COUNT(*) > 1`).Scan(&conflicts).Error
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("B1 migration stopped: historical TRON chain-event conflicts exist (first tx=%s count=%d); resolve explicitly before retry", conflicts[0].RefHash, conflicts[0].Count)
	}
	return nil
}

func Run(db *gorm.DB, initModels []any) error {
	if err := db.AutoMigrate(initModels...); err != nil {
		return err
	}

	options := &gormigrate.Options{TableName: TableName}

	// 旧版升级/全新安装，构建迁移表
	if !db.Migrator().HasTable(TableName) {
		if err := db.Exec("CREATE TABLE " + TableName + " (id VARCHAR(255) PRIMARY KEY)").Error; err != nil {
			return err
		}
	}

	m := gormigrate.New(db, options, migrations)

	return m.Migrate()
}
