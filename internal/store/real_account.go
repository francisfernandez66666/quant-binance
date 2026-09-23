// Package store — SQLite 历史数据存储层。
// real_account.go：实盘账户资产（AUTO_TRADING_PLAN M1 补充）——由广州 QMT 网关
// 在对账时上报的账户级资产（可用资金/冻结资金/总资产/持仓市值）落库，供前端展示。
// 数据源为网关 /api/qmt/report 的 account 事件（broker.query_asset 产出）。
// English: real_account.go — live account assets (available cash / frozen cash / total asset /
// market value) pushed by the Guangzhou QMT gateway's account report event, persisted for display.
// §BINANCE-P2 (PLAN §15.2): rows are keyed by (user_id, market) so CNY/USD/USDT ledgers stay isolated.
package store

import (
	"time"
)

// RealAccount 实盘账户资产行（账户级，非持仓级）。
// §BINANCE-P2（PLAN §15.2）：加 Market 维度——CNY/USD/USDT 三个结算币种各一行，
// 资金闸互不串用；空串归一 CN（存量语义）。
// （RealAccount is one row of live account assets — account-level, per market.）
type RealAccount struct {
	UserID        string  `json:"user_id"`
	Market        string  `json:"market"`         // 市场（CN/US/CRYPTO；空=CN）
	AvailableCash float64 `json:"available_cash"` // 可用资金（可买新股的钱）
	FrozenCash    float64 `json:"frozen_cash"`    // 冻结资金
	TotalAsset    float64 `json:"total_asset"`    // 总资产
	MarketValue   float64 `json:"market_value"`   // 持仓市值
	UpdatedAt     string  `json:"updated_at"`     // 更新时间
}

// realAccountSchemaV2 现行建表语句：PK=(user_id, market)，market 列 DEFAULT 'CN' 兜底遗留写入。
const realAccountSchemaV2 = `CREATE TABLE IF NOT EXISTS real_account (
	user_id       TEXT NOT NULL,
	market        TEXT NOT NULL DEFAULT 'CN',
	available_cash REAL NOT NULL DEFAULT 0,
	frozen_cash  REAL NOT NULL DEFAULT 0,
	total_asset  REAL NOT NULL DEFAULT 0,
	market_value REAL NOT NULL DEFAULT 0,
	updated_at   TEXT NOT NULL,
	PRIMARY KEY (user_id, market)
)`

// ensureRealAccountTable 幂等建表（首次上报时创建）；检出无 market 列的旧版单主键表时
// 原地重建迁移（存量行全部归 CN，行为逐字节不变）。
func (d *DB) ensureRealAccountTable() error {
	var exists int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='real_account'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		_, err := d.db.Exec(realAccountSchemaV2)
		return err
	}
	var cols int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('real_account') WHERE name='market'`).Scan(&cols); err != nil {
		return err
	}
	if cols > 0 {
		return nil
	}
	return d.migrateRealAccountMarket()
}

// migrateRealAccountMarket 旧表（PK=user_id 单列）→ v2（PK=(user_id,market)）重建迁移：
// 币安多币种账户上报前唯一的建表形态，全部行按 CN 语义搬运。
func (d *DB) migrateRealAccountMarket() error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE real_account_v2 (
		user_id       TEXT NOT NULL,
		market        TEXT NOT NULL DEFAULT 'CN',
		available_cash REAL NOT NULL DEFAULT 0,
		frozen_cash  REAL NOT NULL DEFAULT 0,
		total_asset  REAL NOT NULL DEFAULT 0,
		market_value REAL NOT NULL DEFAULT 0,
		updated_at   TEXT NOT NULL,
		PRIMARY KEY (user_id, market)
	)`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO real_account_v2(user_id, market, available_cash, frozen_cash, total_asset, market_value, updated_at)
		SELECT user_id, 'CN', available_cash, frozen_cash, total_asset, market_value, updated_at FROM real_account`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE real_account`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE real_account_v2 RENAME TO real_account`); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertRealAccount 写入/更新账户资产（按 (user_id, market) 幂等；空 Market 归一 CN，
// QMT/交割单链调用零改动即落 CN 行）。
// （UpsertRealAccount writes/updates account assets, idempotent by (user_id, market).）
func (d *DB) UpsertRealAccount(acc RealAccount) error {
	if err := d.ensureRealAccountTable(); err != nil {
		return err
	}
	acc.Market = normalizeMarket(acc.Market)
	if acc.UpdatedAt == "" {
		acc.UpdatedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	_, err := d.db.Exec(`INSERT INTO real_account
		(user_id, market, available_cash, frozen_cash, total_asset, market_value, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, market) DO UPDATE SET
			available_cash=excluded.available_cash, frozen_cash=excluded.frozen_cash,
			total_asset=excluded.total_asset, market_value=excluded.market_value,
			updated_at=excluded.updated_at`,
		acc.UserID, acc.Market, acc.AvailableCash, acc.FrozenCash, acc.TotalAsset, acc.MarketValue, acc.UpdatedAt)
	return err
}

// GetRealAccount 返回指定账号 CN 口径的账户资产（含遗留全局行兜底）；不存在返回零值行。
// （GetRealAccount returns the CN-market account assets for a user; zero row when absent.）
func (d *DB) GetRealAccount(userID string) (RealAccount, error) {
	return d.GetRealAccountForMarket(userID, "CN")
}

// GetRealAccountForMarket §BINANCE-P2（PLAN §15.2）按市场读账户资产行。
// CN 保留「遗留全局行（user_id 空）兜底 + 本账号行优先」的旧口径；
// 非 CN 严格本账号（全局行按迁移语义属 CN，不得越币兜底）。
// English: per-market account row; only CN keeps the legacy global-row fallback.
func (d *DB) GetRealAccountForMarket(userID, market string) (RealAccount, error) {
	m := normalizeMarket(market)
	zero := RealAccount{UserID: userID, Market: m}
	if err := d.ensureRealAccountTable(); err != nil {
		return zero, nil
	}
	var q string
	var args []any
	if m == "CN" {
		q = `SELECT available_cash, frozen_cash, total_asset, market_value, updated_at
			FROM real_account WHERE market='CN' AND (user_id = ? OR user_id = '') ORDER BY user_id DESC LIMIT 1`
		args = []any{userID}
	} else {
		q = `SELECT available_cash, frozen_cash, total_asset, market_value, updated_at
			FROM real_account WHERE market=? AND user_id = ?`
		args = []any{m, userID}
	}
	var acc RealAccount
	acc.UserID = userID
	acc.Market = m
	err := d.db.QueryRow(q, args...).
		Scan(&acc.AvailableCash, &acc.FrozenCash, &acc.TotalAsset, &acc.MarketValue, &acc.UpdatedAt)
	if err != nil {
		// 查不到：返回零值（不报错，前端显示 0/—）
		return zero, nil
	}
	return acc, nil
}
