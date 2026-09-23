// risk_gates.go — §WS-C 风控闸口命中记录与日内已实现盈亏口径。
// 提供 risk_gates 表的命中计数（每 user+日+闸 一行，命中自增，供 SLO/审计/UI 卡片），
// 以及日内已实现盈亏（今日卖出成交 vs 成本）与总资产口径，供 RiskGate 熔断/集中度闸消费。
// English: §WS-C risk-gate hit recording + intraday realized-P&L / total-assets accounting for the
// risk gate (realized-loss circuit breaker and single-stock concentration).
package store

import (
	"fmt"
	"time"
)

// RiskGateHit 单日单闸命中计数行（API/UI 展示用）。
// English: one (user, day, gate) hit-count row.
type RiskGateHit struct {
	UserID     string `json:"user_id"`
	TradeDate  string `json:"trade_date"`
	Gate       string `json:"gate"`
	Hits       int    `json:"hits"`
	LastReason string `json:"last_reason"`
	UpdatedAt  string `json:"updated_at"`
}

// RecordRiskGate 记录一次风控闸命中（幂等自增：每 user+日+闸 一行）。
// English: records one risk-gate hit, incrementing the per (user, day, gate) counter idempotently.
func (d *DB) RecordRiskGate(userID, tradeDate, gate, reason string) error {
	now := time.Now().Format("2006-01-02 15:04:05")
	_, err := d.db.Exec(`INSERT INTO risk_gates (user_id, trade_date, gate, hits, last_reason, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT(user_id, trade_date, gate) DO UPDATE SET
			hits = hits + 1,
			last_reason = excluded.last_reason,
			updated_at = excluded.updated_at`,
		userID, tradeDate, gate, reason, now)
	return err
}

// RiskGateHits 返回某用户某交易日的全部闸口命中计数（gate → hits）。
// English: returns all risk-gate hit counts for one user/day as gate → hits.
func (d *DB) RiskGateHits(userID, tradeDate string) (map[string]int, error) {
	rows, err := d.db.Query(`SELECT gate, hits FROM risk_gates WHERE user_id = ? AND trade_date = ?`, userID, tradeDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var g string
		var h int
		if err := rows.Scan(&g, &h); err != nil {
			return nil, err
		}
		out[g] = h
	}
	return out, rows.Err()
}

// RiskGateDay 返回某交易日的全量命中明细（限行，最新在前）。
// English: returns all risk-gate hit rows for a day, newest first, capped by limit.
func (d *DB) RiskGateDay(tradeDate string, limit int) ([]RiskGateHit, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := d.db.Query(`SELECT user_id, trade_date, gate, hits, last_reason, updated_at
		FROM risk_gates WHERE trade_date = ? ORDER BY id DESC LIMIT ?`, tradeDate, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RiskGateHit
	for rows.Next() {
		var h RiskGateHit
		if err := rows.Scan(&h.UserID, &h.TradeDate, &h.Gate, &h.Hits, &h.LastReason, &h.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CountBuyFilledOrdersByDay 今日已成交买入「笔数」（同一委托去重）——单日买入笔数纪律闸的口径。
//
// 2026-09-18 口径修正：该闸原先统计 orders 表里「今日已报」的委托数（status ∈ 已报/部成/已成），
// 一笔报出去就被券商废掉、或一直挂着没成交的单，同样占掉当天额度——用户会因为一堆根本没成交的
// 报单被锁死买入权。纪律的语义是「今天最多买成几笔」，故改为只数**真实成交**：fills 表是柜台
// 回报落下来的客观事实，同时天然覆盖交割单 sync_fills 补记的手工成交（那些没有本地 orders 行）。
//
// 去重键：同一笔委托被拆成多次部分成交仍算 1 笔——order_id 优先，缺失回落券商交割流水号
// （serial），再缺失回落行 ID（各自算一笔，避免多笔无归因成交被并成 1 笔而少计额度）。
//
// English: today's *filled* buy count for the daily-buy-count discipline gate, deduped per order —
// partial fills of one order count once; keyed by order_id, falling back to the broker serial, then
// to the row id. Filled count (not submitted count) is the ledger-level truth, and it also covers
// broker-only manual fills backfilled by sync_fills (which have no local orders row).
func (d *DB) CountBuyFilledOrdersByDay(userID, day string) (int, error) {
	return d.CountBuyFilledOrdersByDayForMarket(userID, day, "CN")
}

// CountBuyFilledOrdersByDayForMarket §BINANCE-P2（PLAN §15.2）市场作用域口径：
// 只数该市场（US/CRYPTO/CN）的成交笔数——USDT 的成交不能吃 CNY 的单日笔数额度。
// market 空串归一为 CN（存量行 ALTER 回填 'CN'，与无后缀版逐字节同口径）。
// English: market-scoped variant — each settlement currency keeps its own daily buy-count budget.
func (d *DB) CountBuyFilledOrdersByDayForMarket(userID, day, market string) (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(DISTINCT CASE
			WHEN COALESCE(order_id,'') <> '' THEN 'o:' || order_id
			WHEN COALESCE(serial,'')   <> '' THEN 's:' || serial
			ELSE 'r:' || id END)
		FROM fills WHERE user_id=? AND side='买入' AND market=? AND substr(traded_at,1,10)=?`,
		userID, NormalizeMarket(market), day).Scan(&n)
	return n, err
}

// SumBuyFilledAmountByDay 单日已成交买入金额（元）——闸2 单日预算的「已成交」口径。
//
// Σ amount（旧格式回报 amount<=0 时回退 price×qty）。与笔数口径（CountBuyFilledOrdersByDay）
// 同源同表：fills 是柜台回报落下的客观成交事实，交割单 sync_fills 补记的手工成交（无本地
// orders 行）一并计入。「在途冻结」由 LocalBuyFrozen 从 orders 状态派生，两者合起来才是
// 完整冻结账：成交了多少算多少，撤单自动释放，挂单仍占额度。
// English: today's filled buy amount in yuan — the "filled" half of the freeze ledger for the
// daily-budget gate; Σ amount (falling back to price×qty for legacy rows), same fills table as the
// count gate. In-flight freeze comes from LocalBuyFrozen (order-status derived).
func (d *DB) SumBuyFilledAmountByDay(userID, day string) (float64, error) {
	return d.SumBuyFilledAmountByDayForMarket(userID, day, "CN")
}

// SumBuyFilledAmountByDayForMarket §BINANCE-P2（PLAN §15.2）市场作用域买入成交金额。
// English: market-scoped filled-buy amount (per-currency budget ledgers never cross).
func (d *DB) SumBuyFilledAmountByDayForMarket(userID, day, market string) (float64, error) {
	var s float64
	err := d.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN amount>0 THEN amount ELSE price*qty END),0)
		FROM fills WHERE user_id=? AND side='买入' AND market=? AND substr(traded_at,1,10)=?`,
		userID, NormalizeMarket(market), day).Scan(&s)
	return s, err
}

// SumSellFilledAmountByDay 当日卖出成交回款（元）：Σ 卖出 fills 金额（amount 优先，旧数据回落
// price×qty）。冻结账的「回款」半边：卖出即回血——预算闸用回款对冲当日买入占用（上限钳到 0，
// 不因清旧仓放大当日预算），近似资金闸经「持仓成本回落 + 已实现盈亏」两条路体现同一笔回款。
// 手续费未扣（fills 金额为成交额口径），对额度判定偏保守方向无影响——回款少算只会更严不会放水。
// English: today's sell proceeds in yuan — the "replenish" half of the freeze ledger; gates offset
// today's buy occupancy with it (floored at zero so liquidating old inventory never enlarges the
// daily budget). Fees not deducted; undercounting proceeds only tightens, never loosens.
func (d *DB) SumSellFilledAmountByDay(userID, day string) (float64, error) {
	return d.SumSellFilledAmountByDayForMarket(userID, day, "CN")
}

// SumSellFilledAmountByDayForMarket §BINANCE-P2（PLAN §15.2）市场作用域卖出回款。
// English: market-scoped sell proceeds.
func (d *DB) SumSellFilledAmountByDayForMarket(userID, day, market string) (float64, error) {
	var s float64
	err := d.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN amount>0 THEN amount ELSE price*qty END),0)
		FROM fills WHERE user_id=? AND side='卖出' AND market=? AND substr(traded_at,1,10)=?`,
		userID, NormalizeMarket(market), day).Scan(&s)
	return s, err
}

// TodayRealizedPnl 日内已实现盈亏（元，正=盈利，负=亏损）：
// Σ 今日卖出成交 (fillPrice − 成本) × 数量。成本取该 code 当前持仓 CostPrice（已清仓则回落
// 到今日买入均价兜底）；成本不可知（无持仓且无买入成交）时该笔 fail-open 不计入——
// 熔断闸宁可漏计也不因数据缺口误熔断。English: intraday realized P&L in yuan — Σ today's sell fills
// (fillPrice − cost) × qty; cost = the current position's CostPrice, falling back to today's average
// buy price when the position is gone; unknowable cost fails open (not counted) so the breaker never
// trips on a data gap.
func (d *DB) TodayRealizedPnl(userID, day string) (float64, error) {
	return d.TodayRealizedPnlForMarket(userID, day, "CN")
}

// TodayRealizedPnlForMarket §BINANCE-P2（PLAN §15.2）市场作用域已实现盈亏：
// 只汇总该市场当日成交（注意 day 需为「该市场记账时区」的 yyyy-MM-dd，
// 与 gate.marketToday 同口径——CRYPTO=UTC 日、US=纽约日、CN=北京日）。
// 成本回落查持仓按代号互斥形态直接命中（CN 六位+.SH/.SZ、US 字母、CRYPTO 资产对不撞码）。
// §MR-4A：空头侧平仓腿=买入平仓，实现盈亏=(开空均价−平仓价)×数量，成本基准走空头行/
// 当日开空成交均价；多头腿（卖出）口径逐字节不变。
// English: market-scoped realized P&L; day must be the market-local date (same key as gate.marketToday).
// §MR-4A adds the short leg: a 买入平仓 fill realizes (short-cost − cover-price) × qty.
func (d *DB) TodayRealizedPnlForMarket(userID, day, market string) (float64, error) {
	m := NormalizeMarket(market)
	rows, err := d.db.Query(`SELECT code, side, price, qty FROM fills
		WHERE user_id=? AND market=? AND substr(traded_at,1,10)=?`, userID, m, day)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var pnl float64
	for rows.Next() {
		var code, side string
		var price, qty float64
		if err := rows.Scan(&code, &side, &price, &qty); err != nil {
			return 0, err
		}
		switch side {
		case "卖出":
			if qty <= 0 {
				continue
			}
			cost := d.costBasisFor(userID, code, day)
			if cost <= 0 {
				continue // fail-open：成本不可知不计入
			}
			pnl += (price - cost) * qty
		case "买入平仓": // §MR-4A 空头平仓腿：开空均价高于平仓价才是盈利（方向与多头相反）
			if qty <= 0 {
				continue
			}
			cost := d.costBasisForShort(userID, code, day)
			if cost <= 0 {
				continue // fail-open：开空成本不可知不计入
			}
			pnl += (cost - price) * qty
		default:
			continue // 买入/卖出开空是开仓腿，不实现盈亏
		}
	}
	return pnl, rows.Err()
}

// costBasisFor 某 code 的成本价：优先当前持仓 CostPrice；持仓已清时回落今日该 code 买入成交均价。
// §MR-4A：持仓行只认多头（side='long'）——单向持仓下空头行不可能接卖出平仓（成交层已拒），
// 但历史脏数据也要防：拿空头均价当多头成本会把盈亏算反，方向不符直接落到成交均价回落腿。
// English: cost basis for a code — long position CostPrice first; falls back to today's average buy
// fill price when the position is gone. §MR-4A: a short row is never a long's cost basis.
func (d *DB) costBasisFor(userID, code, day string) float64 {
	if p, err := d.RealPositionByCodeForUser(userID, code); err == nil && p.Qty > 0 && p.CostPrice > 0 && p.Side != "short" {
		return p.CostPrice
	}
	fills, err := d.ListFillsByDay(userID, day)
	if err != nil {
		return 0
	}
	var buyAmt, buyQty float64
	for _, f := range fills {
		if f.Side == "买入" && f.Code == code && f.Qty > 0 {
			buyAmt += f.Price * float64(f.Qty)
			buyQty += float64(f.Qty)
		}
	}
	if buyQty <= 0 {
		return 0
	}
	return buyAmt / buyQty
}

// costBasisForShort §MR-4A 空头持仓的成本基准（开空均价）：优先当前空头持仓行 CostPrice
// （ApplyRealFill 对开空腿做加权成本，CostPrice 即加权开空价）；空头行已平完时回落
// 今日该 code 卖出开空成交均价。任何一步不可知返回 0，由调用方 fail-open 不计入。
// English: §MR-4A short cost basis — the short row's CostPrice (weighted short-open price),
// falling back to today's average 卖出开空 fill price; 0 when unknowable (caller fails open).
func (d *DB) costBasisForShort(userID, code, day string) float64 {
	if p, err := d.RealPositionByCodeForUser(userID, code); err == nil && p.Qty > 0 && p.CostPrice > 0 && p.Side == "short" {
		return p.CostPrice
	}
	fills, err := d.ListFillsByDay(userID, day)
	if err != nil {
		return 0
	}
	var openAmt, openQty float64
	for _, f := range fills {
		if f.Side == "卖出开空" && f.Code == code && f.Qty > 0 {
			openAmt += f.Price * float64(f.Qty)
			openQty += float64(f.Qty)
		}
	}
	if openQty <= 0 {
		return 0
	}
	return openAmt / openQty
}

// TotalAssets 当前总资产（元）：可用现金（券商已回报时）+ Σ持仓市值（现价优先，缺失回落成本价）。
// 券商可用资金未回报/不可信时仅计持仓市值（现金口径缺失），由调用方决定是否跳过集中度闸。
// English: current total assets (yuan) — available broker cash (when reported) + Σ position market
// value (live price preferred, cost price fallback). When broker cash is unreported only the held
// value is returned, letting callers skip the concentration gate.
func (d *DB) TotalAssets(userID string) (float64, error) {
	return d.TotalAssetsForMarket(userID, "CN")
}

// TotalAssetsForMarket §BINANCE-P2（PLAN §15.2）市场作用域总资产：
// 现金取该市场账户行（real_account.market），持仓只计该市场行——
// 集中度/熔断闸的分母不再被跨币种行情互相稀释（USDT 持仓不得进 CNY 口径）。
// English: market-scoped total assets — per-market cash row plus that market's positions only.
func (d *DB) TotalAssetsForMarket(userID, market string) (float64, error) {
	m := NormalizeMarket(market)
	total := 0.0
	if acc, err := d.GetRealAccountForMarket(userID, m); err == nil && acc.AvailableCash > 0 {
		total += acc.AvailableCash
	}
	poses, err := d.RealPositionsForUser(userID)
	if err != nil {
		return 0, fmt.Errorf("read real positions: %w", err)
	}
	for _, p := range poses {
		if NormalizeMarket(p.Market) != m {
			continue
		}
		price := p.CurPrice
		if price <= 0 {
			price = p.CostPrice
		}
		// §MR-4A 空头行计**负市值**：开空成交的回款已进账户现金（柜台/交易所上报口径），
		// 持仓侧再按现价计一笔正市值就是把同一笔钱数两次；按 −现价×数量 计恰好是
		// 「现金里趴着开空款 − 买回负债」的权益净额，浮盈浮亏随价格自然反映。
		// CN 恒为多头行（side 缺省 'long'），本分支对 CN 链零影响、逐字节不变。
		// English: §MR-4A short rows contribute −market-value (the open proceeds already sit in
		// reported cash; counting the row positive double-counts). CN rows are always long → byte-identical.
		sign := 1.0
		if p.Side == "short" {
			sign = -1.0
		}
		if price > 0 {
			total += sign * price * float64(p.Qty)
		}
	}
	return total, nil
}
