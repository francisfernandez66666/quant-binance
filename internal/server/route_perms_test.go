// 文件职责：§AUDITFIX926-N10 端点→权限矩阵 golden 静态锁——把「每个 HTTP 路由挂的是哪一级闸」
// 钉成一份可 blame 的 golden 文件（route_perms.json），任何路由的权限档位变化（漏挂、降档、
// 升档、新增端点未评审、删除端点残账）都会让本测试红，直到有人显式重生成 golden（人审义务）。
//
// 缺陷原文（2026-09-26 全量审计 N10 + D3/N5 两案同因）：写端点「裸挂」已被
// write_perms_test.go 的普查锁焊死（除登录引导白名单外必须挂 XxxMiddleware），但那份锁只判
// 「挂没挂」，不判「挂错级别」——该 admin 的挂成 auth 照样绿。D3（币安状态卡：前端全员挂载、
// 后端 admin 闸，成员吃到谎报）与 N5（夜间报告按钮：全员可见、点击才 403）正是「权限档位与
// 消费方口径漂移」的实例，此前全靠人盯路由长尾。
//
// 机制（对齐自家 report_contract_test.go 的「重生成需环境变量+写完即 Skip 强制人审」惯例）：
//
//	· 测试扫描本包全部非 _test.go 文件里的 s.mux.HandleFunc("<方法组> <路径>", <闸>(...)) 注册行；
//	· 每行按外围第一道闸归类档位标签：admin / auth / perm / report / anon；
//	· 与 golden 逐条对比：代码有 golden 无＝新端点未入册（红）；golden 有代码无＝账实不符（红）；
//	  两边都有但档位不同＝权限档位漂移（红）；
//	· 显式重生成：REGEN_ROUTE_PERMS=1 go test ./internal/server -run TestRoutePermsGolden，
//	  写完 golden 即 t.Skip——强制改闸的人经过「重生成+看 diff+人审提交」这一步。
//
// English: golden lock mapping every registered route to its outermost auth gate tier;
// any privilege-level drift (or unregistered new route) turns red until consciously regenerated.
package server

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// routePermsGoldenPath golden 文件路径（与本测试同包目录，入库受版本管理）。
const routePermsGoldenPath = "route_perms.json"

// routeRegLineRe 匹配单行路由注册：HandleFunc("<METHODS> <path>", <expr>)。
// 与 write_perms_test 同一前提——本仓路由必须一行注册完（多行折行会绕开全部既有静态锁）。
var routeRegLineRe = regexp.MustCompile(`HandleFunc\("([A-Z|, ]+) (/[^"]*)", (.*)$`)

// classifyRouteGate 按注册行的闸表达式归类权限档位。判定顺序即优先级：
// 外围第一道闸决定真实门槛（perm/admin 都包在登录态之上，出现即取更强标签）。
func classifyRouteGate(expr string) string {
	switch {
	case strings.Contains(expr, "qmtReportMiddleware("):
		return "report" // 网关 token 认账号的入站回报面（独立鉴权体系）
	case strings.Contains(expr, "adminMiddleware("):
		return "admin"
	case strings.Contains(expr, "permMiddleware("):
		return "perm" // 细粒度权限位（research_approve 等）
	case strings.Contains(expr, "authMiddleware("):
		return "auth"
	default:
		return "anon" // 登录引导/setup/版本检查等匿名面（白名单义务在 write 锁里另有普查）
	}
}

// collectRoutePerms 扫描本包全部非测试 .go 文件，返回 "方法组 路径" → 档位标签。
func collectRoutePerms(t *testing.T) map[string]string {
	t.Helper()
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	got := map[string]string{}
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") { // 注释行留档不参与普查（同 write 锁口径）
				continue
			}
			m := routeRegLineRe.FindStringSubmatch(trimmed)
			if m == nil {
				continue
			}
			key := strings.Join(strings.Fields(m[1]), ",") + " " + m[2] // 方法组去空格：'GET | POST /x'→'GET,|,POST /x' 稳定形态
			got[key] = classifyRouteGate(m[3])
		}
	}
	return got
}

// TestRoutePermsGolden 端点→权限矩阵对账（§AUDITFIX926-N10）。
func TestRoutePermsGolden(t *testing.T) {
	got := collectRoutePerms(t)
	if len(got) < 180 { // 形态守卫：路由面 ~190 条；骤减说明扫描正则被重构绕过（锁自锁）
		t.Fatalf("扫描到 %d 条路由注册，少于形态下限 180——检查 HandleFunc 写法是否折行/变形绕开了锁", len(got))
	}

	// 重生成模式：写 golden 后即 Skip——新档位永远先经过「人主动重生成+审 diff」才入册。
	if os.Getenv("REGEN_ROUTE_PERMS") == "1" {
		// golden 口径：JSON 顶层对象，key 为 "METHOD /path"，value 为档位标签。
		// 手工拼接而非 encoding/json，是为了固定缩进与键序（diff 只看真实档位变化，不被格式化噪声淹没）。
		var b strings.Builder
		b.WriteString("{")
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString("\n  \"")
			b.WriteString(strings.ReplaceAll(k, "\"", "\\\""))
			b.WriteString("\": \"")
			b.WriteString(got[k])
			b.WriteString("\"")
		}
		b.WriteString("\n}\n")
		if err := os.WriteFile(routePermsGoldenPath, []byte(b.String()), 0o644); err != nil {
			t.Fatalf("写 golden 失败: %v", err)
		}
		t.Skipf("已重生成 %s（%d 条）——请 git diff 人审档位变化后提交", routePermsGoldenPath, len(got))
	}

	raw, err := os.ReadFile(routePermsGoldenPath)
	if err != nil {
		t.Fatalf("golden 缺失（%s）：用 REGEN_ROUTE_PERMS=1 go test ./internal/server -run TestRoutePermsGolden 生成并人审", routePermsGoldenPath)
	}
	want := parseRoutePermsJSON(t, raw)

	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("golden 在册但代码已无此注册：%s（档位 %s）——删端点请同步重生成 golden", k, w)
			continue
		}
		if g != w {
			t.Errorf("权限档位漂移：%s golden=%s 代码=%s——升档/降档必须先显式 REGEN 并经人审", k, w, g)
		}
	}
	for k, g := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("新端点未入权限矩阵：%s（档位 %s）——先补评审再 REGEN_ROUTE_PERMS=1 重生成", k, g)
		}
	}
	// 关键档位负锁：审计批降级/裁决过的口径不许被顺手改回去。
	assertGate(t, want, "GET /api/binance/state", "auth")  // §AUDITFIX926-D3 裁决：卡片降级登录可读
	assertGate(t, want, "POST /api/binance/halt", "admin") // 熔断写面维持 admin（D3 只动读面）
	assertGate(t, want, "POST /api/notify-test", "admin")  // §N-2 抬档不许回退
	assertGate(t, want, "POST /api/action", "admin")       // §H3 收权不许回退
}

// assertGate 负锁工具：golden 中某端点档位不符即红（把「历史裁决」钉进矩阵）。
func assertGate(t *testing.T, want map[string]string, key, tier string) {
	t.Helper()
	if got, ok := want[key]; !ok || got != tier {
		t.Errorf("§AUDITFIX926 裁决锁被破坏：%s 期望档位 %s，golden 实际 %v", key, tier, got)
	}
}

// parseRoutePermsJSON 极简解析扁平 {"key": "value"} JSON（不引第三方依赖，键值均无转义复杂度；
// 端点路径含 / 与 |，方法组含逗号——解析按「行一个键值对」形态走，格式由重生成器保证）。
func parseRoutePermsJSON(t *testing.T, raw []byte) map[string]string {
	t.Helper()
	out := map[string]string{}
	lines := strings.Split(string(raw), "\n")
	kv := regexp.MustCompile(`^\s*"(.+)":\s*"([a-z-]+)"?,?\s*$`)
	for _, ln := range lines {
		if ln == "{" || ln == "}" || strings.TrimSpace(ln) == "" {
			continue
		}
		m := kv.FindStringSubmatch(ln)
		if m == nil {
			t.Fatalf("golden 行形态不合解析契约（重生成器被改坏？）: %q", ln)
		}
		out[m[1]] = m[2]
	}
	if len(out) < 180 {
		t.Fatalf("golden 仅 %d 条，少于形态下限 180——文件疑似损坏", len(out))
	}
	return out
}
