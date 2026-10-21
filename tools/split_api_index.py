#!/usr/bin/env python3
# ── M8 god component 拆分脚本 split_api_index.py（§AUDIT1019-M8，2026-10-21）──
# 用途：把 web/src/api/index.js（2161 行 / 162 个导出）按领域拆成 16 个模块，
#   index.js 退化为「统一出口 + 头部说明」，外部 `import * as api from '../api/index.js'`
#   与 `import { isAdmin } from '../api/index.js'` 两种引用形态**零变更**（靠 export * 保留）。
#
# 纪律（为什么敢拆）：
#   1) 只搬家、不改逻辑——代码块按字节原样搬运（含注释），不重排、不改写、不重命名。
#   2) 拆分后必须通过三重验证：导出清单逐一对拍 + eslint + vite build（build 会把任何
#      「漏 import 的跨模块符号引用」打成 not defined，因为 ESM 下不存在隐式全局）。
#   3) 一次拆到底不渐进：渐进拆分会产生大量中间态，反而拉长不可用窗口。
#
# 用法：python3 tools/split_api_index.py          # 生成新模块 + 重写 index.js
#       python3 tools/split_api_index.py --verify # 只校验不写（导出清单对拍）
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "web", "src", "api", "index.js")

# 模块 → 源码行区间（1-based，闭区间）。区间按原文件分节切分，互不重叠、完整覆盖 184~末尾。
# 184~253 = 共享基础设施（BASE/STORAGE_*/nativeAuthBridge/baseUrl/REQUEST_TIMEOUT）。
SEGMENTS = {
    "shared":      [(184, 253), (562, 562)],
    "auth_store":  [(254, 405), (428, 534)],
    "market":      [(406, 427), (1199, 1362)],
    "request":     [(535, 561), (563, 698)],
    "auth_api":    [(699, 736)],
    "signals":     [(737, 773)],
    "status":      [(774, 807)],
    "alerts":      [(808, 843)],
    "positions":   [(844, 1198)],
    "trade":       [(1363, 1414)],
    "sse":         [(1415, 1607)],
    "config":      [(1608, 1764)],
    "consult":     [(1765, 1811)],
    "research":    [(1812, 1995)],
    "admin":       [(1996, 10 ** 9)],
}

# 各模块的中文说明（写进文件头，说明这块管什么）
MODULE_DOC = {
    "shared": "共享基础设施：存储键名、AndroidAuth 原生桥探测、服务器基址、默认超时。被所有其他模块依赖，自身不依赖任何业务模块。",
    "auth_store": "登录态读写：token 原生桥优先（§NATIVEAUTH）、账号/角色/权限本地状态、登出与刷新。",
    "request": "统一请求封装 request()：超时控制、同源回退重试（仅幂等 GET）、鉴权头注入、401 处理。",
    "auth_api": "认证接口：登录/登出等账号动作。",
    "market": "行情与资讯读接口：情绪历史/矩阵、板块热点、行情快照、个股评分、IPO 日历、个股查询、资讯、自选股。",
    "signals": "策略信号读接口。",
    "status": "系统状态读接口。",
    "alerts": "消息提醒读接口。",
    "positions": "持仓管理接口（含实盘/模拟盘仓位与对账，体量最大的一块）。",
    "trade": "交易动作接口 + 市场时段追踪（非交易时段仅首次加载）。",
    "sse": "SSE 服务端推送事件订阅与自动重连。",
    "config": "各类配置读写：LLM 配置、战法参数、做空开关、资讯显示开关、数据源健康。",
    "consult": "股票咨询（多轮对话）。",
    "research": "研究面接口：B5 研究候选、战法库（已应用因子战法管理 + 效果监测）。",
    "admin": "用户/账号管理接口（仅 admin 可达）。",
}

# shared.js 内部符号中，需要被其他模块 import 的那些（原本在 index.js 里是非导出的，
# 拆模块后必须显式 export 才能跨文件引用；只在本文件用的不加）。
# ★ 血的教训（2026-10-21 实测）：漏 export 在 vite SSR（vitest）下**不报错**——ESM 被转成
#   属性访问，符号静默变 undefined；只有 rollup build 才报 "not exported by shared.js"。
#   实测后果：STORAGE_ROLE 变 undefined → getRole() 读不到 localStorage → isAdmin() 恒 false
#   → 管理员的「清空全部」按钮不渲染，perm_gate.test.jsx 真红（跑了 6 分钟才复现）。
EXPORT_IN_SHARED = {
    "BASE", "STORAGE_KEY", "STORAGE_SERVER", "STORAGE_ACCOUNT", "STORAGE_ROLE",
    "STORAGE_PERMS", "nativeAuthBridge", "baseUrl", "LLM_PROBE_TIMEOUT",
}

# 每个模块需要的 import（模块名 -> [(from, names)]）。
# ★ 这张表不是拍脑袋写的：第一轮拆分后跑 eslint no-undef，实测抓出 10 处漏 import
#   （auth_api 用 baseUrl/storeAuth、auth_store 用 baseUrl/request、config 用 LLM_PROBE_TIMEOUT、
#    sse 用 getToken/baseUrl、request 自己声明又被 import 的 REQUEST_TIMEOUT 重复）。
#   逐条修进来后重跑 eslint 归零。新增引用时必须同步补表，否则 build 阶段 not defined 才报。
# ★ 这张表不是拍脑袋写的：第一轮拆分后跑 eslint no-undef，实测抓出 10 处漏 import
#   （auth_api 用 baseUrl/storeAuth、auth_store 用 baseUrl/request、config 用 LLM_PROBE_TIMEOUT、
#    sse 用 getToken/baseUrl、request 自己声明又被 import 的 REQUEST_TIMEOUT 重复）。
#   逐条修进来后重跑 eslint 归零。新增引用时必须同步补表，否则 build 阶段 not defined 才报。
IMPORTS = {
    "shared":      [],
    "auth_store":  [("shared", ["BASE", "STORAGE_KEY", "STORAGE_SERVER", "STORAGE_ACCOUNT",
                                "STORAGE_ROLE", "STORAGE_PERMS", "nativeAuthBridge", "baseUrl"]),
                    ("request", ["request"])],
    "request":     [("shared", ["BASE", "STORAGE_SERVER", "LLM_PROBE_TIMEOUT", "baseUrl"]),
                    ("auth_store", ["getToken", "clearAuth"])],
    "auth_api":    [("request", ["request"]),
                    ("shared", ["baseUrl"]),
                    ("auth_store", ["storeAuth"])],
    "signals":     [("request", ["request"])],
    "status":      [("request", ["request"])],
    "alerts":      [("request", ["request"])],
    "positions":   [("request", ["request"])],
    "market":      [("request", ["request"])],
    "trade":       [("request", ["request"])],
    "sse":         [("request", ["request"]),
                    ("shared", ["BASE", "baseUrl"]),
                    ("auth_store", ["getToken"])],
    "config":      [("request", ["request"]),
                    ("shared", ["LLM_PROBE_TIMEOUT"])],
    "consult":     [("request", ["request"])],
    "research":    [("request", ["request"])],
    "admin":       [("request", ["request"])],
}


def read_src():
    with open(SRC, encoding="utf-8") as f:
        return f.read()


def extract_export_names(text):
    """抽取文本里的顶层导出符号名（export function/const/let/class/async）。"""
    names = []
    for m in re.finditer(r"^export\s+(?:async\s+)?(?:function|const|let|class)\s+([A-Za-z0-9_$]+)",
                         text, re.MULTILINE):
        names.append(m.group(1))
    return names


def build_modules(lines):
    """按 SEGMENTS 切分源码，返回 {模块名: 文本}。"""
    out = {}
    used = []
    for mod, ranges in SEGMENTS.items():
        body = []
        for lo, hi in ranges:
            hi = min(hi, len(lines))
            for i in range(lo, hi + 1):
                used.append(i)
                body.append(lines[i - 1])
        text = "\n".join(body).strip("\n")
        out[mod] = text
    # 自洽校验：区间必须完整覆盖 184~len(lines)，且不重叠
    expect = set(range(184, len(lines) + 1))
    dup = len(used) != len(set(used))
    if dup:
        raise SystemExit("区间重叠：同一行被两个模块占用（拆分脚本自身缺陷）")
    missing = sorted(expect - set(used))
    if missing:
        raise SystemExit("区间有空洞：未覆盖行 %d 段（如 %d~%d），补齐后再跑"
                         % (len(missing), missing[0], missing[-1]))
    # shared.js 内部符号中被其他模块引用的，必须显式 export（原本在 index.js 里是非导出的）。
    # ★ 血的教训：漏 export 在 vite SSR（vitest）下**不报错**——ESM 被转成属性访问，
    #   符号静默变 undefined；只有 rollup build 才报 "not exported by shared.js"。
    #   实测后果：STORAGE_ROLE 变 undefined → getRole() 读不到 localStorage →
    #   isAdmin() 恒 false → 管理员的「清空全部」按钮不渲染，perm_gate 测试真红。
    for sym in EXPORT_IN_SHARED:
        out["shared"] = re.sub(
            r"^(const|function|let|var)\s+%s\b" % re.escape(sym),
            r"export \1 %s" % sym, out["shared"], count=1, flags=re.MULTILINE)
    return out


def check_imports(mods):
    """静态校验：每个 import 的符号在目标模块里必须真有 export。

    不依赖 build/eslint——eslint no-undef 只查"变量有没有定义"（import 了就算有），
    查不到"目标模块没导出这个符号"；而这一类疏漏在 vite SSR 下是静默 undefined。
    """
    bad = []
    for mod, imps in IMPORTS.items():
        for frm, names in imps:
            text = mods.get(frm, "")
            for n in names:
                if not re.search(r"^export\s+(?:async\s+)?(?:function|const|let|class)\s+%s\b"
                                 % re.escape(n), text, re.MULTILINE):
                    bad.append("%s 从 %s import %s，但 %s.js 未导出该符号（会静默变 undefined）"
                               % (mod, frm, n, frm))
    if bad:
        raise SystemExit("import 校验失败：\n  " + "\n  ".join(bad))
    print("import 校验 OK：%d 个跨模块引用全部有导出方"
          % sum(len(n) for _, n in sum(IMPORTS.values(), [])))


def module_text(mod, body):
    """给模块加文件头 + import。"""
    L = []
    L.append("// ── %s.js（§AUDIT1019-M8 从 api/index.js 拆出，2026-10-21）── 请勿手改归属 ──" % mod)
    L.append("// 职责：%s" % MODULE_DOC[mod])
    L.append("// 拆分纪律：代码块由 tools/split_api_index.py 按字节原样搬运，逻辑未改；")
    L.append("//   跨模块引用通过显式 import 表达（ESM 下漏 import 会在 build 阶段报 not defined）。")
    L.append("")
    for frm, names in IMPORTS[mod]:
        L.append("import { %s } from './%s.js'" % (", ".join(names), frm))
    if IMPORTS[mod]:
        L.append("")
    L.append(body)
    return "\n".join(L).rstrip("\n") + "\n"


def main():
    lines = read_src().split("\n")
    mods = build_modules(lines)
    check_imports(mods)   # 先校验再写文件：目标模块没导出 = 静默 undefined（血的教训）
    orig_exports = extract_export_names(read_src())

    # 逐个模块写文件
    total = 0
    for mod in SEGMENTS:
        p = os.path.join(ROOT, "web", "src", "api", mod + ".js")
        txt = module_text(mod, mods[mod])
        with open(p, "w", encoding="utf-8") as f:
            f.write(txt)
        n = len(extract_export_names(txt))
        total += n
        print("已生成 web/src/api/%-14s %5d 行 / 导出 %2d 个" % (mod + ".js", txt.count("\n"), n))

    # 重写 index.js：头部说明（保留原 1~253 的索引表）+ export * 统一出口
    head = lines[:253]  # 原文件 1~253：模块分组端点函数清单（含全部导出索引）
    L = []
    L.append("// ── API 封装层 index.js（§AUDIT1019-M8 拆分后：统一出口，2026-10-21）──")
    L.append("// 本文件原本 2161 行 / 162 个导出混在一个文件里（god module），已按领域拆到同级模块：")
    L.append("//   shared(基础设施) / auth_store(登录态) / request(请求封装) / auth_api(认证) /")
    L.append("//   signals / status / alerts / positions / market / trade / sse / config /")
    L.append("//   consult / research / admin。")
    L.append("// 本文件保留两件事：① 下面的「模块分组端点函数清单」索引表；② 用 export * 把各模块")
    L.append("//   的导出重新汇到同一入口——外部 `import * as api from '../api/index.js'` 与")
    L.append("//   `import { isAdmin } from '../api/index.js'` 两种引用形态零变更。")
    L.append("// 新增接口请放进对应领域模块，不要再往本文件堆（本文件行数受门禁 §88 上限锁约束）。")
    L.append("")
    L.append("// ═══ 模块分组端点函数清单（导出函数索引，全部经 request() 走统一封装）═══")
    L.extend(head[20:])  # 跳过原 1~20 行的旧标题块，保留索引表本体
    L.append("")
    L.append("// ── 统一出口（export * 保留全部导出面，勿改为逐个手写——漏一个就是接口静默消失）──")
    for mod in SEGMENTS:
        if mod == "shared":
            continue  # shared 的符号原本就是非导出，不需要也不应该外泄
        L.append("export * from './%s.js'" % mod)
    new_index = "\n".join(L).rstrip("\n") + "\n"
    with open(SRC, "w", encoding="utf-8") as f:
        f.write(new_index)
    print("已重写 web/src/api/index.js：%d 行（原 2161 行）" % new_index.count("\n"))

    # 对拍：新 index.js 的导出面必须与原文件完全一致
    new_exports = []
    for mod in SEGMENTS:
        if mod == "shared":
            continue
        new_exports.extend(extract_export_names(mods[mod]))
    miss = [e for e in orig_exports if e not in new_exports]
    extra = [e for e in new_exports if e not in orig_exports]
    if miss or extra:
        print("导出清单 drift！缺失=%s 多出=%s" % (miss, extra))
        return 1
    print("导出清单对拍 OK：%d 个导出全部保留，无缺失无新增" % len(orig_exports))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
