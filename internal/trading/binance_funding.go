// 文件职责：§MR-4B 合约资金费轮询腿——BinanceReporter 的第四条协程（仅合约视图存在）。
// USDT 本位永续每 8 小时按标记价偏差结算资金费（00/08/16 UTC），交易所侧表现为
// /fapi/v1/income 的 FUNDING_FEE 流水；本腿按 fundingPollEvery 周期拉流水并交给
// store.AddPositionFundingFee 摊入持仓含费成本基线。
//
// 幂等与窗口设计（与 store 台账同一套账）：
//   - 游标推进：首轮回看 24h（覆盖"重启恰逢结算点"的缺口），之后严格按上轮 endTime 续拉；
//   - 重复无害：窗口重叠时同一条流水由 funding_fees(tran_id) 主键幂等吸收，
//     宁可每轮多查一批空转，也不留漏记缺口（漏记=成本基线偏乐观，方向确定）；
//   - 分页：单轮最多 5 页×1000 条，页间以最后一条 time 推进游标；触顶未消化完
//     保留旧游标（下轮重拉，幂等兜底），绝不静默丢窗。
//
// 降级语义：拉取失败只打日志等下一轮（回报链其余三条腿不受牵连）；现货/美股视图
// 本协程整体不启动（Start 处判定点唯一，零回归）。
//
// English: funding-fee income poller — the 4th leg of the futures reporter. 24h lookback on first
// run, cursor-advancing after; the tran_id ledger makes overlapping windows idempotent. Failures
// degrade to "retry next tick" and never touch the other report legs.
package trading

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// fundingPollEvery 资金费轮询周期：结算节拍是 8h，1h 轮询把入账延迟压到一小时以内，
// 同时权重轻到可以常开（income 端点权重远低于下单面）。
const fundingPollEvery = time.Hour

// fundingFirstRunLookback 首轮回看窗：24h（三个结算周期）——重启跨过结算点也能补齐。
const fundingFirstRunLookback = 24 * time.Hour

// fundingMaxPages 单轮分页上限：防异常数据把协程卡在循环里（触顶即保留游标下轮续）。
const fundingMaxPages = 5

// fundingLoop 资金费协程：立即跑一轮（首启补欠账），之后按节拍走；ctx 收摊即退。
func (r *BinanceReporter) fundingLoop(ctx context.Context) {
	defer r.wg.Done()
	r.pollFundingOnce()
	ticker := time.NewTicker(fundingPollEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.pollFundingOnce()
		}
	}
}

// pollFundingOnce 单轮 income 拉取+入账。游标存协程持有者（reporter 字段），
// 失败不推进——下轮以同窗重放，tran_id 台账保证重放零副作用。
func (r *BinanceReporter) pollFundingOnce() {
	endMs := r.opt.Now().UnixMilli()
	startMs := r.fundingCursor()
	pages := 0
	pageStart := startMs
	for pages < fundingMaxPages {
		pages++
		params := url.Values{
			"incomeType": {"FUNDING_FEE"},
			"startTime":  {fmt.Sprintf("%d", pageStart)},
			"endTime":    {fmt.Sprintf("%d", endMs)},
			"limit":      {"1000"},
		}
		body, err := r.opt.Exec.rawSigned(r.opt.Exec.futures, http.MethodGet, "/fapi/v1/income", params)
		if err != nil {
			log.Printf("[binance] 资金费流水拉取失败（下轮重放，幂等兜底）: %v", err)
			return
		}
		var arr []map[string]any
		if err := binanceUnmarshal(body, &arr); err != nil {
			log.Printf("[binance] 资金费流水解析失败: %v", err)
			return
		}
		for _, it := range arr {
			r.applyFundingIncome(it)
		}
		if len(arr) < 1000 {
			// 本页未触顶=窗口消化完毕：游标推进到本轮 endTime。
			r.setFundingCursor(endMs)
			return
		}
		// 触顶：以最后一条 time+1ms 续页（income 按时间升序返回是文档承诺；
		// 若乱序，最坏是重复页被幂等台账吸收，不产生双摊）。
		last := int64(mapFloat(arr[len(arr)-1], "time"))
		if last <= pageStart {
			r.setFundingCursor(endMs) // 时间戳不前进=异常数据形态，弃本轮窗口推进防死循环
			return
		}
		pageStart = last + 1
	}
	log.Printf("[binance] 资金费单轮分页触顶（%d 页），游标保留待下轮续拉", fundingMaxPages)
}

// applyFundingIncome 单条 income 流水落账：缺 tran_id 的流水直接拒（幂等锚缺失，
// 摊本宁缺勿重）；amount=0 的流水跳过（不产生成本影响，省一次台账位）。
func (r *BinanceReporter) applyFundingIncome(it map[string]any) {
	tranID := int64(mapFloat(it, "tranId"))
	symbol := strings.ToUpper(mapStr(it, "symbol"))
	amount := mapFloat(it, "income")
	ts := int64(mapFloat(it, "time"))
	if tranID == 0 || symbol == "" {
		r.ignored.Add(1)
		return
	}
	if amount == 0 {
		return
	}
	applied, err := r.opt.DB.AddPositionFundingFee(r.opt.UserID, "CRYPTO", symbol, tranID, amount, ts)
	if err != nil {
		log.Printf("[binance] 资金费入账失败 %s tran=%d: %v（下轮重放）", symbol, tranID, err)
		return
	}
	if applied {
		r.fundingApplied.Add(1)
		log.Printf("[binance] 资金费入账 CRYPTO %s amount=%.8f (tran=%d)", symbol, amount, tranID)
	}
}
