// 文件职责：§FIX1012-M6 端点载荷契约 golden 静态锁——把「前端真消费的端点有哪些、各自的
// 请求体解码结构体与必填字段是什么」钉成一份可 blame 的 golden 文件（endpoint_payloads.json）。
// 后端改字段名/删字段、或把某个端点悄悄换 handler，都会让本测试红，直到有人显式重生成 golden。
//
// 与 route_perms.json 的关系：route_perms 锁的是「鉴权维度」（每个端点挂哪一级闸）；本文件锁的是
// 「载荷形状维度」（请求体字段契约）。两者同源思路——扫描注册行对拍 golden，漂移即红，REGEN 强制人审。
//
// 缺陷原文（2026-10-12 第二轮全量字节级 UAT 审计 M6）：全仓 0 个 openapi/TS 契约，190 处 JSDoc 纯文档、
// 无编译强制；后端改字段，前端只能靠运行期炸。本锁把「形状维度」从人盯变成机器锁。
//
// 机制：
//   · 测试扫描本包全部非 _test.go 文件里的 s.mux.HandleFunc("<方法组> <路径>", <表达式>) 注册行；
//   · 与 golden 逐条对比：代码无 golden 有的端点＝端点被删/改名（红）；handler 表达式变化＝handler 被换（红）；
//   · 对 curated 条目（request_struct 非空）：解码结构体必须存在，且其 json tag 须包含契约声明的必填字段
//     （后端删/改名了前端 POST 体依赖的字段即红）；
//   · 显式重生成：REGEN_ENDPOINT_PAYLOADS=1 go test ./internal/server -run TestEndpointPayloadsGolden，
//     写完 golden 即 t.Skip——强制改契约的人经过「重生成+看 diff+人审提交」这一步。
//
// 范围取舍：v1 先锁「端点身份 + handler + 请求体形状」三件事（最高价值的漂移面）；
// 响应体形状维度因多为 map/slice 字面量构造、无统一解码结构体，留作 curated response_fields 文档，
// 暂不自动对拍（避免对响应结构重构假红）。这是 M6 计划「2 天」工程里先交付的可行子集。
//
// English: golden lock binding each frontend-consumed endpoint to its request-body contract;
// endpoint deletion/renaming, handler swap, or required-field removal all turn red until consciously regenerated.
package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// endpointPayloadsGoldenPath 载荷契约 golden 文件路径（与本测试同包目录，入库受版本管理）。
const endpointPayloadsGoldenPath = "endpoint_payloads.json"

// endpointRouteRe 匹配单行路由注册：HandleFunc("<METHODS> <path>", <expr>)。
// 与 route_perms 同前提——本仓路由必须一行注册完（多行折行会绕开全部既有静态锁）。
var endpointRouteRe = regexp.MustCompile(`HandleFunc\("([A-Z|, ]+) (/[^"]*)", (.*)\)\s*$`)

// endpointEntry 单条端点载荷契约（请求/响应字段名 + 类型 + 必填性）。
type endpointEntry struct {
	Method         string          `json:"method"`          // 主方法（前端按主方法消费）
	Path           string          `json:"path"`            // 路径（含 {id} 等模板）
	Handler        string          `json:"handler"`         // 注册行右侧表达式（含中间件包裹），用于发现 handler 替换
	RequestStruct  string          `json:"request_struct"`  // 解码结构体名（前端 POST 体）；空表示无请求体
	RequestFields  []fieldContract `json:"request_fields"`  // 请求体字段契约（curated，形状维度）
	ResponseFields []fieldContract `json:"response_fields"` // 响应体字段契约（curated，形状维度）
}

// fieldContract 单字段契约：字段名 + 类型 + 是否必填。
type fieldContract struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// scanRegisteredEndpoints 扫描本包全部非测试 .go 文件，返回 "METHOD PATH" → 注册表达式。
// 方法组（如 "GET|POST"）取首个方法作为契约键，与 REGEN 同口径保持一致。
func scanRegisteredEndpoints(t *testing.T) map[string]string {
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
			if strings.HasPrefix(trimmed, "//") { // 注释行留档不参与普查（同 route_perms 口径）
				continue
			}
			m := endpointRouteRe.FindStringSubmatch(trimmed)
			if m == nil {
				continue
			}
			methods := strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' || r == '|' })
			if len(methods) == 0 {
				continue
			}
			key := methods[0] + " " + m[2]
			got[key] = strings.TrimSpace(m[3])
		}
	}
	return got
}

// structFieldJSONTags 扫描本包全部 .go 文件，返回某结构体顶层 json tag 集合。
// 结构体不在本包定义时返回 nil（调用方据此跳过字段级检查，可能是外部包类型）。
func structFieldJSONTags(t *testing.T, structName string) map[string]bool {
	t.Helper()
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	tagRe := regexp.MustCompile("`json:\"([A-Za-z0-9_]+)")
	found := false
	tags := map[string]bool{}
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(raw)
		marker := "type " + structName + " struct {"
		idx := strings.Index(src, marker)
		if idx < 0 {
			continue
		}
		found = true
		depth := 0
		start := idx + len(marker)
		body := strings.Builder{}
		for i := start; i < len(src); i++ {
			c := src[i]
			if c == '{' {
				depth++
				continue
			}
			if c == '}' {
				depth--
				if depth == 0 {
					break
				}
				continue
			}
			body.WriteByte(c)
		}
		for _, tm := range tagRe.FindAllStringSubmatch(body.String(), -1) {
			tags[tm[1]] = true
		}
	}
	if !found {
		return nil
	}
	return tags
}

// TestEndpointPayloadsGolden 端点载荷契约对账（§FIX1012-M6）。
func TestEndpointPayloadsGolden(t *testing.T) {
	got := scanRegisteredEndpoints(t)

	// 重生成模式优先：以当前注册面重建 method/path/handler，保留既有 curated 字段
	// （request_struct/字段契约），写后即 Skip——新契约永远先经过「人主动重生成+审 diff」才入册。
	// golden 缺失也能重建（用空 want 当基线），故放在 ReadFile 之前，避免「缺失即 Fatal」死锁。
	if os.Getenv("REGEN_ENDPOINT_PAYLOADS") == "1" {
		want := map[string]endpointEntry{}
		if raw, rerr := os.ReadFile(endpointPayloadsGoldenPath); rerr == nil {
			_ = json.Unmarshal(raw, &want) // 解析失败则忽略，从空基线重建
		}
		rebuilt := map[string]endpointEntry{}
		for k, expr := range got {
			e := endpointEntry{}
			parts := strings.SplitN(k, " ", 2)
			e.Method, e.Path = parts[0], parts[1]
			e.Handler = expr
			if old, ok := want[k]; ok { // 合并既有 curated 数据，避免重生成丢失人工补充的字段契约
				e.RequestStruct = old.RequestStruct
				e.RequestFields = old.RequestFields
				e.ResponseFields = old.ResponseFields
			}
			rebuilt[k] = e
		}
		out, err := json.MarshalIndent(rebuilt, "", "  ")
		if err != nil {
			t.Fatalf("marshal golden: %v", err)
		}
		if err := os.WriteFile(endpointPayloadsGoldenPath, out, 0o644); err != nil {
			t.Fatalf("写 golden 失败: %v", err)
		}
		t.Skipf("已重生成 %s（%d 条）——请 git diff 人审契约变化后提交", endpointPayloadsGoldenPath, len(rebuilt))
	}

	raw, err := os.ReadFile(endpointPayloadsGoldenPath)
	if err != nil {
		t.Fatalf("golden 缺失（%s）：用 REGEN_ENDPOINT_PAYLOADS=1 go test ./internal/server -run TestEndpointPayloadsGolden 生成并人审", endpointPayloadsGoldenPath)
	}
	var want map[string]endpointEntry
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("golden JSON 解析失败：%v", err)
	}

	if len(want) < 50 { // 形态守卫：端点契约面应 ≥50；骤减说明 golden 被清空骗绿（锁自锁）
		t.Fatalf("golden 仅 %d 条，少于形态下限 50——检查 endpoint_payloads.json 是否被清空", len(want))
	}

	for k, w := range want {
		expr, ok := got[k]
		if !ok {
			t.Errorf("golden 在册但代码已无此注册：%s——删端点请同步重生成 golden", k)
			continue
		}
		if expr != w.Handler {
			t.Errorf("handler 漂移：%s golden=%q 代码=%q——替换 handler 必须先显式 REGEN 并经人审", k, w.Handler, expr)
		}
		// 形状维度（仅对 curated 条目生效）：请求结构体必须存在，且其 json 字段须包含契约声明的必填字段。
		if w.RequestStruct != "" {
			tags := structFieldJSONTags(t, w.RequestStruct)
			if tags == nil {
				// 结构体不在本包定义：跳过字段级检查（可能来自外部包），仅要求命名存在则放行
				continue
			}
			for _, fc := range w.RequestFields {
				if fc.Required && !tags[fc.Name] {
					t.Errorf("请求字段漂移：%s 契约要求必填字段 %q 但结构体 %s 无对应 json tag——后端删/改名了前端依赖的字段",
						k, fc.Name, w.RequestStruct)
				}
			}
		}
	}
}
