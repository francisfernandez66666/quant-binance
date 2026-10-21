#!/usr/bin/env python3
# ── FE↔BE 契约类型生成器 gen_api_types.py（§AUDIT1019-M5，2026-10-20）──
# 用途：把后端权威契约 `internal/server/endpoint_payloads.json`（186 条，§FIX1012-M6 产物，
#   由 TestEndpointPayloadsGolden 逐条对拍注册表）转成前端可消费的 TypeScript 声明
#   `web/src/api/types.d.ts`——让「改字段/改路径」从「测试红」升级为「编译红」的一环。
#
# 生成内容（三部分）：
#   1) types.d.ts → ApiEndpointMap（纯 type，键为字面量）/ ApiEndpointKey。
#      request 仅对 golden 里带 request_fields 的端点生成（当前 4 个，见下「覆盖边界」）。
#   2) types.d.ts → 请求体接口：LoginReq / ConsultReq / SetUserRoleReq / UpdateTenantReq
#      （字段名/类型/必填性全部来自 golden 的 request_fields）。
#   3) api_contract_probe.ts → 编译期探针（.ts，见下「★ 两个自缺陷」）。
#
# ★★ 三个自缺陷（2026-10-20 反证时逐个实测发现，改生成器时勿重犯）★★
#   ① `--skipLibCheck` 会跳过**所有声明文件**（.d.ts），即使把它显式传给 tsc 也一样。
#      实证：往 types.d.ts 里注入 `export const b: number = "s"`，tsc 仍返回 0。
#      ⇒ 契约不能靠「直接 tsc 一个 .d.ts」来验证，必须由非声明文件（.ts）引用才被检查。
#   ② `export const M: Record<string, Meta> = {...}` 的显式标注会让 `keyof typeof M` 退化成
#      `string`，于是「任意路径都合法」，断言永远绿。
#      实证：探针断言 `'GET /api/__not_exist__' extends ApiEndpointKey` 竟然为 true。
#      ⇒ 契约表必须是纯 `type`（不写 Record 标注），键才能推断为字面量联合。
#      对策：本生成器只产出 type；并由探针里的 _KeyNotDegraded 断言持续盯住这个退化。
#   ③ 让探针「自己断言自己生成的清单」= 同义反复，锁不住任何事。
#      实证：从 golden 删掉一条端点后重新生成，探针里的元组也同步少一条，仍然全绿。
#      ⇒ 断言的对象必须是**外部事实**：前端真实写死的路径字面量（扫描 web/src 得到）。
#      对策：扫描 web/src 拿真实调用点，只把「当前能在契约中匹配上」的路径固化进断言；
#            匹配不上的列为人审清单（不进断言，避免假红——见「★ 如实边界」）。
#
# ★ 覆盖边界（如实声明，别当成全量类型系统）：
#   golden 的 request_fields 当前只覆盖 4 个端点（其余 182 条为 null）——本生成器的**字段级**
#   契约因此也只有 4 个请求体；但**端点清单级**契约是全覆盖（186 条）。要把字段级覆盖扩到
#   全部端点，需先在 Go 侧补 request_fields（handler 解码结构体反射），属后端独立批次。
#   English: endpoint-level contract is complete (186); request-field-level contract covers only
#   the 4 endpoints whose golden entries carry request_fields.
#
# 用法：
#   python3 tools/gen_api_types.py              # 生成两个文件（覆盖写）
#   python3 tools/gen_api_types.py --check      # 只对拍不写：与入库版本有 drift 即退出 1（门禁用）
#   REGEN_API_TYPES=1 python3 tools/gen_api_types.py   # 同默认行为；显式声明"我在重新生成"
#
# 配套（必须由仓库持有，缺一即闸门失效）：
#   npm run typecheck            # tsc 检查**探针**（不是 .d.ts），契约漂移/退化即编译红
#
# 纪律：生成物**入库**（gitignore 只排除 *.md/*.svg/.uat-data，不影响这两个文件），
#   因此"重新生成后必须人审 diff"由 --check 对拍锁强制——改 Go 契约不重新生成就会红。
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
GOLDEN = os.path.join(ROOT, "internal", "server", "endpoint_payloads.json")
OUT = os.path.join(ROOT, "web", "src", "api", "types.d.ts")
PROBE = os.path.join(ROOT, "web", "src", "api", "api_contract_probe.ts")
# ★ 前端路径「基线」：进编译断言的路径集合固化在这里，不随每次生成重新判定。
#   为什么必须有它：若每次生成都现场判定"哪些路径在契约里"，后端删掉某端点后该路径会
#   被自动移出断言清单，探针仍然全绿——等于闸门自愈失效（2026-10-20 反证踩到）。
#   有了基线，元组固定不变，后端删端点 ⇒ 该路径不在 ApiPath ⇒ 编译红。
#   反向也锁住：前端新增调用点而未更新基线 ⇒ --check 报错，强制人审后更新。
BASELINE = os.path.join(ROOT, "internal", "server", "frontend_paths_baseline.json")

# Go 类型 → TS 类型（golden 的 request_fields.type 取值域）
GO2TS = {
    "string": "string",
    "bool": "boolean",
    "object": "Record<string, unknown>",
    "array": "unknown[]",
    "number": "number",
    "float64": "number",
    "float32": "number",
    "int": "number",
    "int64": "number",
}

# Go 结构体名 → TS 接口名（golden 只给 Go 结构体名，这里做稳定映射；未知结构体走兜底命名）
STRUCT2IFACE = {
    "loginReq": "LoginReq",
    "consultReq": "ConsultReq",
    "setUserRoleReq": "SetUserRoleReq",
    "updateTenantReq": "UpdateTenantReq",
}


# 前端源码里形如 '/api/xxx' 的字符串字面量（三引号正则同时兼容 ' " ` 三种引号）
FE_PATH_RE = re.compile(r"""(['"`])(/api/[^'"`]*)\1""")
# 测试/mock 目录不是运行时调用点（测试里可能故意写不存在的路径做负向断言），排除。
SKIP_DIRS = {"node_modules", "dist", ".git", "coverage", "__tests__", "__mocks__"}
# 生成物自身必须排除——否则"断言自己扫出来的自己"，又变回自指的同义反复。
SKIP_FILES = {"api_contract_probe.ts", "types.d.ts"}


def norm_path(s: str) -> str:
    """归一化前端写死的路径：截掉 query、去掉结尾斜杠。

    前端存在 `'/api/x/' + id`、`'/api/kline?symbol='` 这类拼接残片，不归一化会永远
    匹配不上契约，进而让断言假红——归一化是「不假红」的前提。
    """
    s = s.split("?", 1)[0].split("#", 1)[0]
    if len(s) > 1 and s.endswith("/"):
        s = s.rstrip("/")
    return s


def scan_frontend_paths():
    """扫描 web/src 下前端真实调用后端的路径字面量。

    返回 (exact, pattern_only, unmatched, stats)：
      exact        —— 与契约 path **字面量完全相等**（进编译断言；TS 侧 ApiPath 也是字面量
                      联合，两边语义必须一致，否则要么假红要么假绿）
      pattern_only —— 只能靠 {param} 通配匹配上（前端写死了具体 id 等），**不进断言**
      unmatched    —— 契约里没有（人审清单，不进断言，避免假红）
      stats        —— 统计字典，用于如实声明覆盖边界
    """
    src = os.path.join(ROOT, "web", "src")
    contract_paths = _contract_literal_paths()
    raw = {}      # 归一化路径 -> [出现位置]
    n_files = 0
    n_lit = 0
    n_tpl = 0
    for dp, dirs, files in os.walk(src):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        for fn in files:
            if not fn.endswith((".js", ".jsx", ".ts", ".tsx")):
                continue
            if fn in SKIP_FILES:
                continue
            n_files += 1
            fp = os.path.join(dp, fn)
            try:
                with open(fp, encoding="utf-8", errors="ignore") as f:
                    lines = f.readlines()
            except OSError:
                continue
            rel = os.path.relpath(fp, ROOT)
            for i, line in enumerate(lines, 1):
                for m in FE_PATH_RE.finditer(line):
                    n_lit += 1
                    lit = m.group(2)
                    if "${" in lit or "{" in lit:
                        n_tpl += 1      # 模板串（含变量）无法做字面量断言，如实跳过
                        continue
                    p = norm_path(lit)
                    if p and p.startswith("/api/"):
                        raw.setdefault(p, []).append("%s:%d" % (rel, i))
    exact_set = set(p for p in raw if p in contract_paths)
    exact = sorted(exact_set)
    pattern_only = sorted((p, raw[p][0]) for p in raw
                          if p not in exact_set and _in_contract(p))
    unmatched = sorted((p, raw[p][0]) for p in raw
                       if p not in exact_set and not _in_contract(p))
    stats = {
        "files": n_files, "literals": n_lit, "template_skipped": n_tpl,
        "unique": len(raw), "matched": len(exact),
        "pattern_only": len(pattern_only), "unmatched": len(unmatched),
    }
    return exact, pattern_only, unmatched, stats


def _contract_patterns():
    """把契约路径（含 {param} 占位）编译成可 fullmatch 的正则，供前端路径匹配。"""
    with open(GOLDEN, encoding="utf-8") as f:
        golden = json.load(f)
    pats = []
    for v in golden.values():
        p = v.get("path", "")
        if not p:
            continue
        # {param} 段按「任意非斜杠串」通配，其余段按字面量转义
        parts = []
        for seg in re.split(r"(\{[^}]*\})", p):
            parts.append(r"[^/]+" if seg.startswith("{") and seg.endswith("}") else re.escape(seg))
        pats.append(re.compile("^" + "".join(parts) + "$"))
    return pats


def _contract_literal_paths():
    """契约里 path 的**字面量**集合（含 {param} 原样占位，不做通配展开）。

    TS 侧的 `ApiPath` 就是这个集合的字面量联合，因此 Python 侧判定"是否入断言"必须用同一
    套字面量语义——早先用通配匹配导致 `'…/optimizations/7/approve'` 被判为匹配、而 TS 侧
    不在 ApiPath 中，探针假红（2026-10-20 实测）。
    """
    with open(GOLDEN, encoding="utf-8") as f:
        golden = json.load(f)
    return set(v.get("path", "") for v in golden.values() if v.get("path"))


_CONTRACT_PATS_CACHE = None


def _in_contract(path: str) -> bool:
    """前端归一化路径是否存在于契约（{param} 段按通配匹配）。"""
    global _CONTRACT_PATS_CACHE
    if _CONTRACT_PATS_CACHE is None:
        _CONTRACT_PATS_CACHE = _contract_patterns()
    return any(rx.match(path) for rx in _CONTRACT_PATS_CACHE)


def iface_name(struct: str) -> str:
    """Go 结构体名 → TS 接口名：已知映射优先，否则首字母大写兜底。"""
    if struct in STRUCT2IFACE:
        return STRUCT2IFACE[struct]
    return struct[:1].upper() + struct[1:] if struct else "UnknownReq"


def ts_type(go_type: str) -> str:
    return GO2TS.get(go_type, "unknown")


def build() -> str:
    with open(GOLDEN, encoding="utf-8") as f:
        golden = json.load(f)

    endpoints = []          # (method, path, struct_or_empty, fields_or_None)
    ifaces = {}             # iface_name -> [(field, ts_type, required)]
    for key, v in golden.items():
        method = v.get("method", "")
        path = v.get("path", "")
        struct = (v.get("request_struct") or "").strip()
        fields = v.get("request_fields")
        endpoints.append((method, path, struct, fields))
        if fields:
            name = iface_name(struct)
            # 同一结构体被多端点共用时合并（字段集应一致；不一致以先到者为准并保留注释警示）
            if name in ifaces and ifaces[name] != [
                (fl.get("name", ""), ts_type(fl.get("type", "")), bool(fl.get("required")))
                for fl in fields
            ]:
                raise SystemExit(
                    "生成中止：结构体 %s 在不同端点的字段集不一致（golden 自身漂移），"
                    "先修 golden 再生成" % struct
                )
            ifaces[name] = [
                (fl.get("name", ""), ts_type(fl.get("type", "")), bool(fl.get("required")))
                for fl in fields
            ]

    n_ep = len(endpoints)
    n_iface = len(ifaces)
    n_field_ep = sum(1 for _, _, _, fl in endpoints if fl)

    lines = []
    lines.append("// ── AUTO-GENERATED by tools/gen_api_types.py（§AUDIT1019-M5）── 请勿手改 ──")
    lines.append("// 来源：internal/server/endpoint_payloads.json（%d 条端点契约，§FIX1012-M6）" % n_ep)
    lines.append("// 重新生成：REGEN_API_TYPES=1 python3 tools/gen_api_types.py（人审 diff 后提交）")
    lines.append("// 对拍锁：python3 tools/gen_api_types.py --check（drift 即红，进门禁 §87）")
    lines.append("//")
    lines.append("// ★ 覆盖边界（如实声明）：")
    lines.append("//   - 端点清单级契约 = 全覆盖（%d 条）；前端路径字面量可用 keyof ApiEndpointMap 约束。" % n_ep)
    lines.append("//   - 请求字段级契约 = 仅 %d 个端点（golden 的 request_fields 只覆盖这些）；" % n_field_ep)
    lines.append("//     其余端点的请求体尚未字段化，属后端独立批次（Go 侧补结构体反射）。")
    lines.append("//   - 本文件不含运行时代码（纯 type 声明），不参与打包体积。")
    lines.append("//")
    lines.append("// ★ 形态约束（反证实证，勿改回 const + Record 标注）：")
    lines.append("//   - 契约表必须是 type 而非 const：--skipLibCheck 跳过 .d.ts，且 .d.ts 中不允许初始化器；")
    lines.append("//   - 不要加 `Record<string, ApiEndpointMeta>` 标注：那会让 keyof 退化成 string，断言永远绿。")
    lines.append("//   - 真正的编译期检查由 api_contract_probe.ts（.ts，非声明文件）承载。")
    lines.append("")
    lines.append("/** 单个端点的契约描述。 */")
    lines.append("export interface ApiEndpointMeta {")
    lines.append("  /** HTTP 方法（大写）。 */")
    lines.append("  method: string")
    lines.append("  /** 端点路径（与后端 mux.HandleFunc 注册串一致，含 {param} 占位）。 */")
    lines.append("  path: string")
    lines.append("  /** 有字段级契约时的请求体接口名；无则 undefined（路径仍受清单约束）。 */")
    lines.append("  request?: string")
    lines.append("}")
    lines.append("")
    lines.append("/**")
    lines.append(" * 全部端点契约表：key = \"METHOD /path\"（后端注册串的唯一权威形态）。")
    lines.append(" * 用法示例（约束路径字面量，写错即编译错）：")
    lines.append(" *   import type { ApiEndpointKey } from '@/api/types'")
    lines.append(" *   const ep: ApiEndpointKey = 'GET /api/status'   // 拼错即 TS2322")
    lines.append(" *")
    lines.append(" * ★ 用 type 而非 const：.d.ts 中不允许初始化器，且必须让键推断为字面量联合；")
    lines.append(" *   若写成 `const M: Record<string, Meta>`，keyof 会退化成 string（已实证）。")
    lines.append(" */")
    lines.append("export type ApiEndpointMap = {")
    for method, path, struct, fields in sorted(endpoints, key=lambda x: (x[1], x[0])):
        req = iface_name(struct) if fields else ""
        if req:
            lines.append("  '%s %s': { method: '%s'; path: '%s'; request: '%s' }"
                         % (method, path, method, path, req))
        else:
            lines.append("  '%s %s': { method: '%s'; path: '%s' }" % (method, path, method, path))
    lines.append("}")
    lines.append("")
    lines.append("/** 端点 key 联合类型（\"METHOD /path\"）——前端拼路径时的编译期约束。 */")
    lines.append("export type ApiEndpointKey = keyof ApiEndpointMap")
    lines.append("")
    lines.append("/**")
    lines.append(" * 端点**路径**联合类型（不含 METHOD，含 {param} 占位形态）。")
    lines.append(" * 前端写死路径（`api.get('/api/x')`）时用这个约束：契约里删掉该路径即编译红。")
    lines.append(" */")
    lines.append("export type ApiPath = ApiEndpointMap[keyof ApiEndpointMap]['path']")
    lines.append("")
    for name in sorted(ifaces):
        lines.append("/**")
        lines.append(" * %s —— 请求体契约（字段/必填性来自后端 golden request_fields）。" % name)
        lines.append(" */")
        lines.append("export interface %s {" % name)
        for fname, ftype, required in ifaces[name]:
            opt = "" if required else "?"
            lines.append("  /** %s */" % ("必填" if required else "可选"))
            lines.append("  %s%s: %s" % (fname, opt, ftype))
        lines.append("}")
        lines.append("")
    lines.append("// 生成统计：端点 %d 条 / 请求体接口 %d 个 / 字段级覆盖端点 %d 个"
                 % (n_ep, n_iface, n_field_ep))
    lines.append("")
    return ("\n".join(lines), endpoints, ifaces, n_ep, n_field_ep)


def load_baseline():
    """读取固化的前端路径基线（无文件则返回 None，由 main 负责初始化）。"""
    if not os.path.isfile(BASELINE):
        return None
    with open(BASELINE, encoding="utf-8") as f:
        data = json.load(f)
    return sorted(data.get("paths", []))


def save_baseline(paths):
    """写入基线（含生成时间说明与来源，便于人审 diff）。"""
    payload = {
        "_comment": (
            "前端真实调用后端的路径基线（AUTO-GENERATED，勿手改）：进编译断言的清单固化为"
            "本文件，后端从 endpoint_payloads.json 删掉任一路径即编译红。"
            "前端新增调用点时需 UPDATE_FE_BASELINE=1 python3 tools/gen_api_types.py 更新并人审。"
        ),
        "paths": sorted(paths),
    }
    with open(BASELINE, "w", encoding="utf-8") as f:
        json.dump(payload, f, ensure_ascii=False, indent=2)
        f.write("\n")
    return os.path.relpath(BASELINE, ROOT)


def build_probe(endpoints, ifaces, n_ep, n_field_ep, locked, matched, pattern_only,
                unmatched, stats) -> str:
    """生成「编译期探针」api_contract_probe.ts（.ts，非声明文件）。

    locked —— 固化基线（真正进断言的清单）；matched —— 本次扫描结果（仅用于统计与提示）。

    为什么必须存在这个文件：tsc 的 --skipLibCheck 会跳过所有 .d.ts（实证：往 types.d.ts
    注入 `export const b: number = "s"` 后 tsc 仍返回 0），所以契约类型必须被一个 .ts 文件
    引用，检查才真正发生。本文件只导出 type，编译后为空、不进 bundle（未被任何运行时
    模块 import）。
    """
    L = []
    L.append("// ── AUTO-GENERATED by tools/gen_api_types.py（§AUDIT1019-M5）── 请勿手改 ──")
    L.append("// ★ 本文件是「编译期探针」，存在的唯一理由：")
    L.append("//   --skipLibCheck 会跳过所有 .d.ts（已实证：注入类型错误后 tsc 仍返回 0），")
    L.append("//   所以必须由这个非声明文件（.ts）引用契约类型，tsc 才会真正检查。")
    L.append("//   本文件只导出 type，无运行时产物、不进 bundle（运行时模块不 import 它）。")
    L.append("// 检查入口：npm run typecheck（web/package.json，门禁 §87）")
    L.append("")
    L.append("import type { ApiEndpointKey, ApiPath } from './types'")
    for name in sorted(ifaces):
        L.append("import type { %s } from './types'" % name)
    L.append("")
    L.append("/** 编译期断言工具：T 不是 true 就编译红（TS2344）。 */")
    L.append("type Expect<T extends true> = T")
    L.append("")
    L.append("/** 退化哨兵：ApiEndpointKey 若退化成 string（例如契约表被写成 Record<string, …>），返回 true。 */")
    L.append("type Degraded<T> = string extends T ? true : false")
    L.append("")
    L.append("// ── 断言 1：契约 key 未退化成 string ──")
    L.append("// 历史缺陷：契约表写成 `const M: Record<string, ApiEndpointMeta>` 时，keyof 退化为 string，")
    L.append("// 于是任意路径都「合法」，下面断言 2 会永远绿。此断言专门盯住该回归。")
    L.append("// ⚠ 取 false 而非 never：`Expect<never>` 不会报错（never 是任何类型的子类型），")
    L.append("//   写成 never 会让这条哨兵失效（已自查发现）。")
    L.append("export type _KeyNotDegraded = Degraded<ApiEndpointKey> extends true ? false : true")
    L.append("type _A1 = Expect<_KeyNotDegraded>")
    L.append("")
    L.append("// ── 断言 2：契约清单自身自洽（作用边界如实声明）──")
    L.append("// ⚠ 这条**锁不住**「后端删端点」：本元组与 types.d.ts 同源于 golden，")
    L.append("//   golden 少一条时本元组会同步少一条，必然自洽（2026-10-20 反证实测仍绿）。")
    L.append("// 它的真实作用：只生成了 types.d.ts 却没生成本文件（或其一被手改）时，这里才红。")
    L.append("// 真正锁「后端删端点」的是断言 3（对象是前端真实写死的路径，属外部事实）。")
    L.append("export type _AllEndpointsInContract = Expect<")
    L.append("  [")
    for method, path, _s, _f in sorted(endpoints, key=lambda x: (x[1], x[0])):
        L.append("    '%s %s'," % (method, path))
    L.append("  ] extends readonly ApiEndpointKey[] ? true : false")
    L.append(">")
    L.append("")
    L.append("// ── 断言 3（真正有效）：前端真实调用点必须仍在契约内 ──")
    L.append("// 清单来源：**固化的基线** internal/server/frontend_paths_baseline.json（外部事实，")
    L.append("// 不随每次生成重判——否则后端删掉端点后该路径会被自动移出清单，闸门自愈失效）。")
    L.append("// 因此后端从 endpoint_payloads.json 删掉任一被前端使用的路径，这里即编译红。")
    L.append("// 覆盖边界：基线 %d 条；本次扫描 %d 个唯一路径，其中 %d 个与契约 path 字面量相等。" % (
        len(locked), stats["unique"], stats["matched"]))
    L.append("//           %d 个仅 {param} 通配命中（前端写死具体 id 等）、%d 个契约中无，" % (
        stats["pattern_only"], stats["unmatched"]))
    L.append("//           两者列在文件末尾供人审，刻意**不进断言**——否则闸门假红，等于失效。")
    L.append("export type _FrontendPathsInContract = Expect<")
    L.append("  [")
    for p in locked:
        L.append("    '%s'," % p)
    L.append("  ] extends readonly ApiPath[] ? true : false")
    L.append(">")
    L.append("")
    if pattern_only:
        L.append("// ── 人审清单 A：只能靠 {param} 通配命中（%d 条，不进编译断言）──" % len(pattern_only))
        L.append("// 前端写死了具体 id/编号，契约里是 {param} 占位；TS 侧 ApiPath 是字面量联合，")
        L.append("// 无法表达通配，进断言必然假红，故只列不锁。")
        for p, loc in pattern_only:
            L.append("//   %-46s ← %s" % (p, loc))
        L.append("")
    if unmatched:
        L.append("// ── 人审清单 B：契约里查无此路径（%d 条，不进编译断言）──" % len(unmatched))
        L.append("// 可能原因：① 前端拼接残片（如 '/api/x/' + id）；② 契约遗漏（后端应补 golden）；")
        L.append("//           ③ 前端独有/第三方路由。逐条判明前不要把它们加进断言，否则闸门假红。")
        for p, loc in unmatched:
            L.append("//   %-46s ← %s" % (p, loc))
        L.append("")
    if ifaces:
        L.append("// ── 断言 4：%d 个请求体的字段级契约形状 ──" % n_field_ep)
        L.append("// 后端改字段（改名/改类型/必填性变化）未重新生成，这里即编译红。")
        for name in sorted(ifaces):
            shape = "; ".join(
                "%s%s: %s" % (fn, "" if req else "?", ft) for fn, ft, req in ifaces[name]
            )
            L.append("export type _%sShape = Expect<{ %s } extends %s ? true : false> "
                     "// 后端契约形状" % (name, shape, name))
        L.append("")
    L.append("// 生成统计：端点 %d 条 / 请求体接口 %d 个" % (n_ep, len(ifaces)))
    L.append("")
    return "\n".join(L)


def main() -> int:
    types_txt, endpoints, ifaces, n_ep, n_field_ep = build()
    matched, pattern_only, unmatched, stats = scan_frontend_paths()
    sys.stderr.write(
        "前端路径扫描：%d 文件 / %d 处字面量 / %d 处含模板已跳过 → 唯一 %d 个；"
        "字面量命中契约 %d 个 / 仅通配命中 %d 个 / 契约中无 %d 个（后两者仅人审）\n"
        % (stats["files"], stats["literals"], stats["template_skipped"],
           stats["unique"], stats["matched"], stats["pattern_only"], stats["unmatched"])
    )

    # 基线：默认读取固化清单；UPDATE_FE_BASELINE=1（或首次运行）时用本次扫描结果重写。
    update = os.environ.get("UPDATE_FE_BASELINE") == "1"
    locked = None if update else load_baseline()
    if locked is None:
        locked = matched
        rel = save_baseline(locked)
        sys.stderr.write("已%s前端路径基线 %s（%d 条）——请人审 diff 后提交\n"
                         % ("更新" if update else "初始化", rel, len(locked)))
    probe_txt = build_probe(endpoints, ifaces, n_ep, n_field_ep, locked,
                            matched, pattern_only, unmatched, stats)
    check = "--check" in sys.argv
    if check:
        bad = False
        # 先判基线：基线 drift 时生成物必然也要重生成，先报根因，避免"两条报错指向同一件事"。
        bl = load_baseline()
        if bl is None:
            sys.stderr.write(
                "基线 %s 缺失——跑 UPDATE_FE_BASELINE=1 python3 tools/gen_api_types.py 生成并人审\n"
                % os.path.relpath(BASELINE, ROOT))
            return 1
        if set(bl) != set(matched):
            added = sorted(set(matched) - set(bl))
            gone = sorted(set(bl) - set(matched))
            sys.stderr.write("前端路径基线 drift：\n")
            if added:
                sys.stderr.write("  新增（前端加了调用点，需更新基线）: %s\n" % ", ".join(added))
            if gone:
                sys.stderr.write("  消失（后端删了端点或前端删了调用，必须先判明是哪一种）: %s\n"
                                 % ", ".join(gone))
            sys.stderr.write("  处理：后端真删端点 → 先让编译红跑完、确认前端已同步下线；"
                             "前端真新增 → UPDATE_FE_BASELINE=1 重新生成并人审 diff\n")
            return 1
        for path, want, label in ((OUT, types_txt, "types.d.ts"),
                                  (PROBE, probe_txt, "api_contract_probe.ts")):
            if not os.path.isfile(path):
                sys.stderr.write("%s 缺失（未生成过）——跑 REGEN_API_TYPES=1 python3 tools/gen_api_types.py\n"
                                 % label)
                bad = True
                continue
            with open(path, encoding="utf-8") as f:
                cur = f.read()
            if cur != want:
                sys.stderr.write(
                    "%s 与后端契约 drift（Go 契约变了没重新生成，或生成物被手改）——"
                    "先 REGEN_API_TYPES=1 python3 tools/gen_api_types.py 再人审 diff\n" % label
                )
                bad = True
        if bad:
            return 1
        print("ok - types.d.ts + api_contract_probe.ts + 前端路径基线 三方一致（无 drift）")
        return 0
    for path, txt in ((OUT, types_txt), (PROBE, probe_txt)):
        with open(path, "w", encoding="utf-8") as f:
            f.write(txt)
        print("已生成 %s（%d 字节）" % (os.path.relpath(path, ROOT), len(txt.encode("utf-8"))))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
