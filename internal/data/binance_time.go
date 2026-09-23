// 文件职责：币安数据源的**时间戳口径归一 + JSON 取值工具**（Phase 3 第 1 项，PLAN §2.8）。
//
// normalizeBinanceEpoch 是全仓库唯一的时间换算出口：PLAN §2.8 记录三种口径并存
// （epoch 毫秒 / epoch 微秒 / data.binance.vision 归档的"自 2025-01-01 起的微秒"）。
// 口径必须由调用点**显式声明**，禁止靠数量级猜——毫秒值与 vision 微秒值落在同一量级区间
// （1.7e12 vs 2.3e13），猜必错且错得安静。
//
// fieldNum/firstInt64Any 是"未实测契约"的兼容层（GAP §G-1：美股端点须首尔出口实测）：
// 按候选键顺序取第一个正数，全非正返回 0=未知（0 与"源真给 0"在下游一律按未知处理）。
package data

import (
	"encoding/json"
	"strconv"
	"time"
)

// BinanceEpochUnit 源时间戳口径（空串按最普遍的毫秒处理）。
type BinanceEpochUnit string

const (
	BinanceEpochMS      BinanceEpochUnit = "ms"        // epoch 毫秒（现货 REST/WS 缺省）
	BinanceEpochUS      BinanceEpochUnit = "us"        // epoch 微秒
	BinanceEpochSeconds BinanceEpochUnit = "s"         // epoch 秒
	BinanceEpochVision  BinanceEpochUnit = "vision_us" // 自 2025-01-01 起的微秒（归档源口径）
)

// normalizeBinanceEpoch 源时间戳 → UTC time.Time。v<=0 或口径未知返回零值：
// 零值=未知，调用方必须判 IsZero 后降级，绝不当 1970-01-01 参与计算。
func normalizeBinanceEpoch(v int64, unit BinanceEpochUnit) time.Time {
	if v <= 0 {
		return time.Time{}
	}
	switch unit {
	case BinanceEpochUS:
		return time.UnixMicro(v).UTC()
	case BinanceEpochSeconds:
		return time.Unix(v, 0).UTC()
	case BinanceEpochVision:
		return binanceVisionEpochBase.Add(time.Duration(v) * time.Microsecond).UTC()
	default:
		return time.UnixMilli(v).UTC()
	}
}

// fieldNum 按候选键顺序取第一个正数值（number/string 通吃；全非正则 0=未知）。
func fieldNum(m map[string]any, keys ...string) float64 {
	for _, k := range keys {
		if raw, ok := m[k].(string); ok {
			if f := jsonFloat(raw); f > 0 {
				return f
			}
			continue
		}
		if f := anyFloat(m[k]); f > 0 {
			return f
		}
	}
	return 0
}

// firstInt64Any 按候选键顺序取第一个正整数（时间戳键名未实测时兼容）。
func firstInt64Any(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		if v := anyFloat(m[k]); v > 0 {
			return int64(v)
		}
	}
	return 0
}

// mapAnyString 安全取字符串字段（键缺失或类型不符返回 ""；数字形态转成十进制串）。
func mapAnyString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch s := v.(type) {
		case string:
			return s
		case float64:
			return strconv.FormatFloat(s, 'f', -1, 64)
		}
	}
	return ""
}

// decodeBinanceJSON 解码辅助：单测把 httptest body 直接喂给结构体，省掉重复的 error 样板。
func decodeBinanceJSON(raw []byte, out any) error { return json.Unmarshal(raw, out) }
