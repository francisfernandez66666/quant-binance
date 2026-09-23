// 文件职责：BrokerConfig 接口层单测（P1-a）——QMTConfig 访问器映射逐项断言 + 代码形态市场推断消歧表。
// 回归意义：访问器必须与结构体字段一一对应（零行为变化的可证形式）；推断顺序锁定 CN→US点分→CRYPTO尾缀→US纯码→CN缺省。
package config

import (
	"testing"
)

// TestQMTConfigBrokerAccessors §P1-a BrokerConfig 接口锁：QMTConfig 逐键转发
// （enabled/mode/halted/撤单秒/固定金额等）与视图方法必须与原直读字段口径一致——
// Controller 泛化消费后，这里钉住「CN 行为字节不变」的配置面契约。
func TestQMTConfigBrokerAccessors(t *testing.T) {
	t1 := true // halted 显式置位样本（区分 false 零值与未设置）
	q := QMTConfig{
		Enabled:           true,
		Mode:              "auto",
		Halted:            true,
		CancelStaleSec:    300,
		CloseSweepAt:      1452,
		MissHeartbeatSec:  120,
		FixedAmount:       10000,
		MaxPositions:      10,
		DailyMaxBuys:      20,
		DailyBudgetAmount: 100000,
		InitialCapital:    200000,
		Strategies:        []string{"factor", "pattern"},
		Blacklist:         []string{"300001"},
		RiskGate:          RiskGateConfig{MaxOrderAmount: 50000},
		EnforceT1:         &t1,
	}
	var b BrokerConfig = q
	if !b.BrokerEnabled() || b.BrokerMode() != "auto" || !b.BrokerHalted() {
		t.Fatalf("开关/模式访问器映射错误: %v %q %v", b.BrokerEnabled(), b.BrokerMode(), b.BrokerHalted())
	}
	if b.BrokerCancelStaleSec() != 300 || b.BrokerCloseSweepAt() != 1452 || b.BrokerMissHeartbeat() != 120 {
		t.Fatalf("时钟类访问器映射错误: %d %d %d", b.BrokerCancelStaleSec(), b.BrokerCloseSweepAt(), b.BrokerMissHeartbeat())
	}
	if b.BrokerFixedAmount() != 10000 || b.BrokerDailyBudget() != 100000 || b.BrokerInitialCapital() != 200000 {
		t.Fatalf("金额类访问器映射错误: %g %g %g", b.BrokerFixedAmount(), b.BrokerDailyBudget(), b.BrokerInitialCapital())
	}
	if b.BrokerMaxPositions() != 10 || b.BrokerDailyMaxBuys() != 20 {
		t.Fatalf("上限类访问器映射错误: %d %d", b.BrokerMaxPositions(), b.BrokerDailyMaxBuys())
	}
	if len(b.BrokerStrategies()) != 2 || len(b.BrokerBlacklist()) != 1 {
		t.Fatalf("清单类访问器映射错误: %v %v", b.BrokerStrategies(), b.BrokerBlacklist())
	}
	if b.BrokerRiskGate().MaxOrderAmount != 50000 {
		t.Fatalf("BrokerRiskGate 映射错误: %+v", b.BrokerRiskGate())
	}
	if !b.BrokerEnforceT1() {
		t.Fatal("BrokerEnforceT1 应为 true（显式开启）")
	}
	// EnforceT1=nil 时默认开启（现状语义），访问器必须同源。
	if !(QMTConfig{}).BrokerEnforceT1() {
		t.Fatal("BrokerEnforceT1 零值缺省应为 true（与 EnforceT1Enabled 一致）")
	}
}

func TestInferMarketOf(t *testing.T) {
	cases := []struct{ code, want string }{
		{"600519.SH", "CN"},
		{"000001.SZ", "CN"},
		{"000001.BJ", "CN"},
		{"BTCUSDT", "CRYPTO"},
		{"btcusdt", "CRYPTO"}, // 小写先归一
		{"ETHUSDC", "CRYPTO"},
		{"ALGOBTC", "CRYPTO"},
		{"AAPL", "US"},
		{"BRK.B", "US"},
		{"SPY", "US"},
		{"TSLA", "US"},
		{"600519", "CN"},   // 裸 6 位数字缺省 CN（不满足 US 正则首字母要求）
		{"", "CN"},         // 空码缺省 CN（存量默认市场）
		{"BTCUSD.T", "US"}, // 含点非 CN → US 分支（异常形态由 validTsCode 拒，推断不报错）
	}
	for _, c := range cases {
		if got := InferMarketOf(c.code); got != c.want {
			t.Errorf("InferMarketOf(%q)=%s want %s", c.code, got, c.want)
		}
	}
}
