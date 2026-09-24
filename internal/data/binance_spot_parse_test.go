// 文件职责：现货 WS 解析器的**真实帧形态**回归锁（2026-09-24 §市场分家-1 附带缺陷修复）。
// 事故原型：@miniTicker/@ticker 真实帧必带事件名 "e"（字符串），而解析结构体的时间戳
// 字段标签是 `json:"E" int64`——encoding/json 的大小写不敏感回退匹配让 "e" 撞进 "E"，
// 每一帧都判解析失败。既有测试全部用注入解析器或无 "e" 的手工帧，从未吃过真机形状，
// 这条缺口一直静默。本文件锁死三件事：
//
//	① 带 "e" 的真实帧必须解析成功（字段逐项核对）；
//	② "E" 时间戳必须正确落位 TimeMs（不是被 "e" 顶掉的 0）；
//	③ @ticker 的涨跌幅 P 为负数时照常取值。
//
// English: regression locks for REAL Binance spot WS frames (which always carry the
// event-name key "e"); before this fix the case-insensitive JSON fallback collided
// "e" into the int64 "E" field and silently dropped every production frame.
package data

import (
	"math"
	"testing"
)

// TestParseSpotMiniTickerRealFrame 真实 24hrMiniTicker 帧（含 "e" 事件名）逐字段核对。
func TestParseSpotMiniTickerRealFrame(t *testing.T) {
	raw := []byte(`{"e":"24hrMiniTicker","E":1758682800123,"s":"btcusdt","o":"49000.1","c":"50000.5","h":"50100","l":"48900","v":"123.45","Q":"6000000.7"}`)
	tick, err := parseSpotMiniTicker(raw)
	if err != nil {
		t.Fatalf("真实帧（带 e）必须解析成功: %v", err)
	}
	if tick.Symbol != "BTCUSDT" || tick.Price != 50000.5 {
		t.Fatalf("symbol/price 不符: %+v", tick)
	}
	if tick.TimeMs != 1758682800123 {
		t.Fatalf("TimeMs 必须取 E，实为 %d（被 e 撞字段则是 0）", tick.TimeMs)
	}
	if want := (50000.5/49000.1 - 1) * 100; math.Abs(tick.ChangePct-want) > 1e-9 {
		t.Fatalf("miniTicker 涨跌幅近似口径错: %v vs %v", tick.ChangePct, want)
	}
	if tick.PrevClose != 0 {
		t.Fatalf("miniTicker 无昨收，PrevClose 必须留 0，实为 %v", tick.PrevClose)
	}
}

// TestParseSpotTickerRealFrameNegPct 真实 24hrTicker 帧：负涨跌幅照常 + o 落 PrevClose。
func TestParseSpotTickerRealFrameNegPct(t *testing.T) {
	raw := []byte(`{"e":"24hrTicker","E":1758682800456,"s":"ETHUSDT","o":"3000","c":"2940","P":"-2.0","Q":"88000","v":"30","h":"3050","l":"2900"}`)
	tick, err := parseSpotTicker(raw)
	if err != nil {
		t.Fatalf("真实 ticker 帧必须解析成功: %v", err)
	}
	if tick.ChangePct != -2.0 {
		t.Fatalf("负涨跌幅必须原样取 P，实为 %v", tick.ChangePct)
	}
	if tick.PrevClose != 3000 {
		t.Fatalf("24hr ticker 的 o=可信参考价 → PrevClose=3000，实为 %v", tick.PrevClose)
	}
	if tick.TimeMs != 1758682800456 {
		t.Fatalf("TimeMs 应取 E，实为 %d", tick.TimeMs)
	}
}

// 端到端（feed 缺省解析器下的帧命中数断言）在 internal/engine/binance_quote_test.go
// 的 TestQuoteSourceFeedFirst 覆盖——那里用真 feed+真帧，命中数=1 即证伪"永远 0 命中"。
