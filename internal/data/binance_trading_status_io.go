// 文件职责：交易状态**帧解析入口**（配合 binance_trading_status.go 的证据库本体）。
// 单独成文件是因为"未实测契约"的容错解析最容易膨胀：PLAN §2.4 只给了流名，没给字段口径
// （GAP §G-1 须首尔出口实测），所以把键名宽容匹配集中一处，实测后只改这张键名表。
package data

import (
	"bytes"
	"encoding/json"
	"strings"
)

// jsonShapeBinance 状态帧形态宽容判别：币安状态流可能推单对象或数组（批量/合并推送，
// 口径未实测前两种都收）。返回 (对象指针或 nil, 元素数组或 nil)；解析失败/空 → 双 nil。
// 键值一律按标准 json.Unmarshal 落 float64（配套 mapAnyString/fieldNum 的数字形态分支）。
func jsonShapeBinance(raw []byte) (*map[string]any, []map[string]any) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return nil, nil
	}
	if t[0] == '[' {
		var arr []map[string]any
		if json.Unmarshal(t, &arr) != nil {
			return nil, nil
		}
		return nil, arr
	}
	if t[0] != '{' {
		return nil, nil
	}
	var obj map[string]any
	if json.Unmarshal(t, &obj) != nil {
		return nil, nil
	}
	return &obj, nil
}

// HandlePayload 解析一帧 tradingStatus/tradability 并入库；返回入库条数（0=与本库无关）。
// 组合流按 stream 名分流；裸流靠 payload 里的状态键自证（认不出就返回 0，绝不误吞行情帧）。
func (s *BinanceTradingStatus) HandlePayload(payload []byte) int {
	stream, data, err := ParseBinanceCombinedStream(payload)
	if err != nil {
		s.logThrottled("状态帧信封解析失败: %v", err)
		return 0
	}
	low := strings.ToLower(stream)
	switch {
	// ⚠ 分流比较一律用「常量的 lower 形态」：流名在币安侧是小写（aapl@tradingstatus），
	// 而常量 EquityStreamTradingStatus 带驼峰（"tradingStatus"）。曾直接 "@"+常量 拼进
	// lower 分支 → tradingStatus 分支恒不可达、在龄证据永远为零（§9 闸被喂成全员"未确认"
	// 拒单，US 链形同熔断）。两侧必须同大小写口径，此处锁死（Agent B 缺陷①修复）。
	case strings.HasSuffix(low, "@"+strings.ToLower(EquityStreamTradingStatus)):
		return s.ingestStatus(data, false)
	case strings.HasSuffix(low, "@"+strings.ToLower(EquityStreamTradability)):
		return s.ingestStatus(data, true)
	case low == "":
		if n := s.ingestStatus(data, false); n > 0 { // 裸流：tradingStatus 优先
			return n
		}
		return s.ingestStatus(data, true)
	default:
		return 0
	}
}

// ingestStatus 入库一条（对象形态）或一批（数组形态）状态证据。
func (s *BinanceTradingStatus) ingestStatus(raw []byte, isTradability bool) int {
	obj, arr := jsonShapeBinance(raw)
	if obj == nil && len(arr) == 0 {
		return 0
	}
	n := 0
	if obj != nil && s.putStatus(*obj, isTradability) {
		n++
	}
	for i := range arr {
		if s.putStatus(arr[i], isTradability) {
			n++
		}
	}
	return n
}

// putStatus 单条状态映射入库；symbol/state 认不出即 false（不写脏证据）。
func (s *BinanceTradingStatus) putStatus(m map[string]any, isTradability bool) bool {
	sym := normalizeBinanceSymbol(mapAnyString(m, "S", "symbol", "Symbol", "code"))
	state := strings.ToUpper(strings.TrimSpace(mapAnyString(m,
		"tradingStatus", "status", "state", "ts", "marketStatus", "s")))
	if sym == "" || state == "" {
		return false
	}
	ev := binanceStatusEvidence{
		State:   state,
		Detail:  strings.TrimSpace(mapAnyString(m, "reason", "r", "limitReason", "restriction")),
		SeenAt:  s.now(),
		EventMs: int64(fieldNum(m, "E", "eventTime", "timestamp", "t")),
	}
	s.mu.Lock()
	if isTradability {
		s.trad[sym] = ev
	} else {
		s.trade[sym] = ev
	}
	delete(s.misses, sym)
	s.watched[sym] = true
	s.mu.Unlock()
	return true
}
