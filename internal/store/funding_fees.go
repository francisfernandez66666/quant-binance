// Package store — funding_fees.go：§MR-4B USDT 本位永续资金费入账腿。
//
// 职责：把 fapi /fapi/v1/income 拉回的 FUNDING_FEE 流水（每 8 小时结算一次，交易所
// 真扣/真收现金）折进实盘持仓的含费成本基准（§F1 含费口径），并以前缀幂等台账
// funding_fees(tran_id 主键) 防重放——资金费窗口允许 24h 回看自愈重启缺口，
// 同一条流水二次到达必须被吸收而不是二次摊本。
//
// 口径推导（与 ApplyRealFill 的费率摊本同族）：
//   - 摊本额 paid = −amount（交易所 income 原值：负=支出、正=收取）；
//   - 多空同式：cost_price += paid/qty、amount = cost_price×qty——开仓佣金在
//     ApplyRealFill 里对多空两向都做的是"基准加费"，资金费沿用同一数学形状，
//     两腿口径分裂比符号选择更危险（对账/浮盈展示只认含费基准一条线）；
//   - 无持仓行/qty≤0：台账照落、成本不动（现金变动已在交易所余额里，对账腿会兜住）。
//
// 零回归边界：本方法只被币安合约回报接收器调用；CN/QMT 链不存在 funding 概念，
// 表与判重路径永不被触达（建表幂等、加列非破坏）。
//
// English: folds perpetual funding-fee income rows into the fee-inclusive cost basis of
// real_positions, idempotent via the funding_fees(tran_id) ledger so a 24h replay window is safe.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AddPositionFundingFee 记一笔资金费流水并摊入持仓成本。
// 返回 applied=true 表示这是首次落账（成本已调整）；false 表示 tran_id 幂等命中
// （历史已摊，不再动账）。userID 空=遗留全局行路径（与成交腿同款谓词形状）。
func (d *DB) AddPositionFundingFee(userID, market, tsCode string, tranID int64, amount float64, incomeTime int64) (bool, error) {
	if tranID == 0 {
		return false, errors.New("funding 流水缺少 tran_id（幂等锚缺失，宁可不记）")
	}
	market = normalizeMarket(market)
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// 幂等锚：INSERT OR IGNORE——主键冲突即"这条流水已处理过"，静默成功且不动成本。
	res, err := tx.Exec(`INSERT OR IGNORE INTO funding_fees(tran_id, user_id, market, ts_code, amount, income_time, applied_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		tranID, userID, market, tsCode, amount, incomeTime, time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		return false, fmt.Errorf("funding 台账落库失败: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil // 幂等命中（重放窗口内的老流水），账本不动
	}
	// 遗留全局行认领：与 ApplyRealFill 同谓词形状，保证多租户下摊本打在对的账号行上。
	if userID != "" {
		if _, err := tx.Exec(`UPDATE real_positions SET user_id=? WHERE market=? AND ts_code=? AND user_id=''`,
			userID, market, tsCode); err != nil {
			return false, fmt.Errorf("funding 认领遗留行失败: %w", err)
		}
	}
	var qty, cost float64
	qerr := tx.QueryRow(`SELECT qty, cost_price FROM real_positions WHERE market=? AND ts_code=? AND (user_id='' OR user_id=?)`,
		market, tsCode, userID).Scan(&qty, &cost)
	if errors.Is(qerr, sql.ErrNoRows) {
		// 无仓可摊：流水仍算已处理（防止持仓重建后被历史窗口二次摊入）。
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if qerr != nil {
		return false, fmt.Errorf("funding 读持仓失败: %w", qerr)
	}
	if qty > 0 {
		// 摊本：paid=−income.amount（支出→基准上抬），与开仓佣金的基准调整同族。
		paid := -amount
		newCost := cost + paid/qty
		if _, err := tx.Exec(`UPDATE real_positions SET cost_price=?, amount=?, updated_at=?
			WHERE market=? AND ts_code=? AND (user_id='' OR user_id=?)`,
			newCost, newCost*qty, time.Now().Format("2006-01-02 15:04:05"), market, tsCode, userID); err != nil {
			return false, fmt.Errorf("funding 摊本失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// FundingFee 资金费流水的最小投影（funding_fees 行）。
type FundingFee struct {
	TranID     int64
	Amount     float64
	IncomeTime int64
}

// FundingFeesForPosition 读某持仓的资金费流水（观测/对账用，最新在前，limit 兜底）。
func (d *DB) FundingFeesForPosition(userID, market, tsCode string, limit int) ([]FundingFee, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.Query(`SELECT tran_id, amount, income_time FROM funding_fees
		WHERE user_id=? AND market=? AND ts_code=? ORDER BY income_time DESC LIMIT ?`,
		userID, normalizeMarket(market), tsCode, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FundingFee
	for rows.Next() {
		var v FundingFee
		if err := rows.Scan(&v.TranID, &v.Amount, &v.IncomeTime); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
