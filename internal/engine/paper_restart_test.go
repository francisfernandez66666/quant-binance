package engine

import (
	"path/filepath"
	"testing"

	"quant-trading-v2/internal/config"
	"quant-trading-v2/internal/paper"
)

// TestPaperConfigSurvivesRestart §AUDIT1019-M1 行为锁（第三轮审计新发现 P1：
// "paper 撮合配置重启不恢复"）。
//
// 场景复现（与生产缺陷逐环对应）：
//  1. 全局 rules.paper.enabled=false（config.json 缺省）；账号 u1 经 POST /api/paper/config
//     开启（等价 SetPaperConfigFor，store 非空 ⇒ 写 per-user auth store，不碰全局）；
//  2. "重启"：用同一配置文件路径 + 同一 KVStore 新建 Manager 与 Registry
//     （全局配置仍为 false——这正是缺陷根因：boot 只读全局）；
//  3. GetPaper("u1") 必须返回 enabled=true 的引擎。
//
// 反证由测试内对照组承担：若回放逻辑（registry.go GetPaper 内
// `GetRulesFor(userID).Paper.Enabled → ConfigFromRules` 一段）被删掉，
// 第 3 步拿到的是 boot 模板（enabled=false）⇒ 本测试红。对照组断言先证明
// "模板确实是关的"，排除"模板本来就开着所以恒绿"的假绿可能。
// English: per-user paper config must survive a process restart — the boot path used to read
// only the global config.json while the write path persisted per-user, so the switch was lost.
func TestPaperConfigSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	kv := &memKV{} // 复用 fingerprint_test.go 的同款极简 KVStore（跨 Manager 实例存活=模拟 auth.json 持久）

	// ── 第一次"运行"：开启前 ──
	mgr := config.NewManager(cfgPath)
	mgr.SetStore(kv)
	if mgr.Rules.Paper.Enabled {
		t.Fatalf("前置假设被破坏：全局 rules.paper.enabled 缺省应为 false（缺省关才是缺陷场景）")
	}

	// 等价 POST /api/paper/config {enabled:true, fixed_amount:5000}
	mgr.SetPaperConfigFor("u1", func(p *config.PaperConfig) {
		p.Enabled = true
		p.FixedAmount = 5000
	})
	// 热更新路径的同款读取：per-user 已开启
	if r := mgr.GetRulesFor("u1"); r == nil || !r.Paper.Enabled {
		t.Fatalf("SetPaperConfigFor 后 GetRulesFor(u1).Paper.Enabled 应为 true")
	}
	// 对照组①：全局未被污染（写的是 per-user，不是 config.json）
	if mgr.Rules.Paper.Enabled {
		t.Fatalf("对照组①失败：per-user 写入不应改动全局 rules.paper.enabled")
	}

	// ── "重启"：同一配置文件 + 同一 KVStore，全新 Manager / Registry ──
	mgr2 := config.NewManager(cfgPath)
	mgr2.SetStore(kv)
	// 对照组②：重启后全局仍是 false ⇒ boot 模板（生产形态 main.go:246）确实是关的，
	// 因此第 3 步能过只能靠 GetPaper 内的 per-user 回放，不可能是模板自带。
	if mgr2.Rules.Paper.Enabled {
		t.Fatalf("对照组②失败：重启后全局 rules.paper.enabled 应仍为 false（否则测不到回放）")
	}
	// boot 装配口径与 cmd/quant/main.go:246-248 一致：全局 Rules.Paper → 模板引擎
	tmpl := paper.New(paper.ConfigFromRules(mgr2.Rules.Paper), "")
	if tmpl.Enabled() {
		t.Fatalf("对照组②失败：boot 模板引擎应为关闭态")
	}
	r2 := NewRegistry(EngineOptions{Paper: tmpl, CfgMgr: mgr2, DataDir: dir})

	// ── 断言：重启后懒加载的账号引擎必须是开启态，且 per-user 参数被带回 ──
	pe := r2.GetPaper("u1")
	if pe == nil {
		t.Fatalf("GetPaper(u1) 返回 nil（回放后账号引擎应被创建）")
	}
	if !pe.Enabled() {
		t.Fatalf("§AUDIT1019-M1 回归：重启后账号 u1 模拟盘应为开启态（boot 只读全局导致开启态丢失）")
	}
	if got := pe.Cfg().FixedAmount; got != 5000 {
		t.Fatalf("重启后 per-user 参数应被带回：FixedAmount=%v, 期望 5000", got)
	}

	// ── 反向语义：未配置账号不得被误开（userRules 回落全局副本）──
	if other := r2.GetPaper("u_untouched"); other != nil && other.Enabled() {
		t.Fatalf("未配置账号 u_untouched 不应被误开（回落全局模板才是正确语义）")
	}
}
