#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
独立字节级 UAT · 第二轮（2026-10-12）

与 tools/indep_byte_uat.py（2026-09-27 版）完全独立的第二套断言。
不复用被测项目任何断言函数；不复用上一版脚本的任何代码路径。

本轮新增的、上一版没有的断言族：
  A. BE↔BE 字节级对拍：API 写入 → SQLite 直读原文（逐字节）
                          SQLite 直写 → API 读回（逐字节）
     载体：PUT/GET /api/research/sweep-pools 的 strategy 字段
           （store.sweep_pool_configs.strategy 是 TEXT 主键，落在 trading.db）
  B. 字节级注入面：SQL 注入 / 超长串 / 换行 tab / 代理环境
  C. 账号隔离：低权 + 匿名 对 perm 端点的拒绝语义
  D. 会话安全：登出真实吊销 + 伪造令牌
  E. 错误信封统一性（字节级 JSON 结构）
  F. FE↔BE：前端 API 路径 vs 后端路由断链扫描（独立实现，不借项目代码）
  G. 幂等：同 PUT 两次 combos 一致

环境注意（2026-10-12 实测）：沙箱注入 HTTP_PROXY=http://127.0.0.1:57928，
Python urllib 访问 localhost 会被代理劫持并返回空 —— 这是环境产物，
不是产品缺陷。本脚本用 ProxyHandler({}) 显式绕过，凡绕过后才通的断言一律如实标注。
"""
import hashlib
import json
import os
import sqlite3
import sys
import time
import urllib.error
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BASE = os.environ.get("E2E_API", "http://127.0.0.1:18080")
FRONT = os.environ.get("E2E_BASE_URL", "http://localhost:5173")
DB = os.path.join(ROOT, ".uat-data", "trading.db")
ADMIN_U, ADMIN_P = "admin", "Nightly!2026"
TESTER_U, TESTER_P = "tester", "Tester!2026"

PASS, FAIL, WARN = [], [], []


def rec(ok, name, detail=""):
    (PASS if ok else FAIL).append((name, detail))
    print(("  PASS  " if ok else "  FAIL  ") + name + ((" :: " + detail) if detail else ""))


def warn(name, detail):
    WARN.append((name, detail))
    print("  WARN  " + name + " :: " + detail)


# ── 显式绕过沙箱代理（环境产物，非产品缺陷）────────────────────────────────
OPENER = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def http(method, path, token=None, body=None, raw_body=None, timeout=15, host=None,
         accept="application/json"):
    url = (host or BASE) + path
    data = None
    if raw_body is not None:
        data = raw_body.encode("utf-8") if isinstance(raw_body, str) else raw_body
    elif body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Accept", accept)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    try:
        with OPENER.open(req, timeout=timeout) as r:
            return r.status, r.read(), dict(r.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read(), dict(e.headers)
    except Exception as e:
        return 0, str(e).encode(), {}


def login(u, p):
    st, b, _ = http("POST", "/api/auth/login", body={"username": u, "password": p})
    try:
        return json.loads(b.decode("utf-8")).get("token", "")
    except Exception:
        return ""


def dbq(sql, args=()):
    """独立 SQLite 直连（不复用项目 store 层）。"""
    c = sqlite3.connect(DB, timeout=10)
    try:
        return c.execute(sql, args).fetchall()
    finally:
        c.close()


print("=" * 78)
print("独立字节级 UAT 第二轮 · quant-trading-v2 / " + time.strftime("%Y-%m-%d %H:%M:%S"))
print("=" * 78)

# ── 0. 探活 ────────────────────────────────────────────────────────────────
print("\n[0] 环境探活")
ADMIN = login(ADMIN_U, ADMIN_P)
TESTER = login(TESTER_U, TESTER_P)
rec(len(ADMIN) == 64, "admin 登录返回 64 字符令牌（绕过沙箱代理后）", f"len={len(ADMIN)}")
rec(len(TESTER) == 64, "tester 登录返回 64 字符令牌", f"len={len(TESTER)}")
if not ADMIN or not TESTER:
    print("  !! 令牌获取失败，无法继续。")
    sys.exit(2)
st, b, _ = http("GET", "/api/auth/me", token=ADMIN)
me = json.loads(b.decode()) if st == 200 else {}
rec(me.get("role") == "admin", "admin 上下文 role=admin", str(me.get("role")))
st, b, _ = http("GET", "/api/auth/me", token=TESTER)
me2 = json.loads(b.decode()) if st == 200 else {}
rec(me2.get("role") == "user", "tester 上下文 role=user（低权）", str(me2.get("role")))

# 一个合法的基础配置（保证 Validate 通过：各维 step>0、to>=from、组合数<100000）
BASE_POOL = {
    "tp_from": 3, "tp_to": 6, "tp_step": 1,
    "sl_from": 2, "sl_to": 5, "sl_step": 1,
    "hold_from": 1, "hold_to": 3, "hold_step": 1,
    "score_from": 60, "score_to": 70, "score_step": 5,
    "atr_from": 0, "atr_to": 0, "atr_step": 0,
}

TS = str(int(time.time()))
MARK = f"IBV2-{TS}"
NASTY = f'{MARK}-特殊<&"\'>中文é€😀\n\t换行与制表'
SQLINJ = f"{MARK}-'; DROP TABLE sweep_pool_configs;--"
LONGNAME = MARK + "-" + "长" * 3000


def put_pool(name, token=ADMIN):
    p = dict(BASE_POOL)
    p["strategy"] = name
    return http("PUT", "/api/research/sweep-pools", token=token, body=p)


def get_pool_names(token=ADMIN):
    st, b, _ = http("GET", "/api/research/sweep-pools", token=token)
    if st != 200:
        return st, None
    try:
        return st, [x.get("strategy") for x in json.loads(b.decode()).get("pools", [])]
    except Exception:
        return st, None


# ── A. BE↔BE 字节级对拍 ────────────────────────────────────────────────────
print("\n[A] BE↔BE 字节级对拍（API写→SQLite直读 / SQLite直写→API读回）")

# A1 正向：API 写入 → 直读 SQLite 原文，逐字节
st, b, _ = put_pool(NASTY)
rec(st == 200, "PUT 含特殊字符的 strategy 成功（服务端接受）", f"http={st} {b[:100]}")
if st == 200:
    rows = dbq("SELECT strategy FROM sweep_pool_configs WHERE strategy=?", (NASTY,))
    got = rows[0][0] if rows else None
    rec(got == NASTY, "SQLite 直读原文与请求体逐字符相等（无截断/无转义错位）",
        f"期望len={len(NASTY)} 实得len={len(got) if got else 0}")
    if got is not None:
        rec(got.encode("utf-8") == NASTY.encode("utf-8"),
            "UTF-8 字节序列逐字节相等",
            f"sha256 期望={hashlib.sha256(NASTY.encode()).hexdigest()[:16]} "
            f"实得={hashlib.sha256(got.encode()).hexdigest()[:16]}")
    # 经 API 读回
    st2, names = get_pool_names()
    rec(st2 == 200 and NASTY in (names or []), "GET /api/research/sweep-pools 读回同一字符串",
        f"http={st2} 命中={NASTY in (names or [])}")

# A2 反向：SQLite 直写 → API 读回，逐字节
REV = f'{MARK}-反向直写<&">é😀\n\t'
c = sqlite3.connect(DB, timeout=10)
c.execute("INSERT OR REPLACE INTO sweep_pool_configs "
          "(strategy,tp_from,tp_to,tp_step,sl_from,sl_to,sl_step,hold_from,hold_to,hold_step,"
          "score_from,score_to,score_step,atr_from,atr_to,atr_step,updated_at) "
          "VALUES (?,3,6,1,2,5,1,1,3,1,60,70,5,0,0,0,?)", (REV, "2026-10-12 00:00:00"))
c.commit()
c.close()
st, names = get_pool_names()
rec(st == 200 and REV in (names or []),
    "SQLite 直写 → API 读回逐字符相等（双向通道闭合）",
    f"http={st} 命中={REV in (names or [])}")

# A3 超长字符串往返
st, b, _ = put_pool(LONGNAME)
rec(st == 200, f"PUT 超长 strategy（{len(LONGNAME)} 字符）成功", f"http={st}")
if st == 200:
    rows = dbq("SELECT strategy FROM sweep_pool_configs WHERE strategy=?", (LONGNAME,))
    rec(bool(rows) and rows[0][0] == LONGNAME, "超长串 SQLite 直读逐字符相等",
        f"实得len={len(rows[0][0]) if rows else 0}")

# A4 SQL 注入：表必须仍在，值必须原样存回
st, b, _ = put_pool(SQLINJ)
rec(st == 200, "PUT 含 SQL 注入串的 strategy 被接受（按普通字符串处理）", f"http={st} {b[:80]}")
tbls = [r[0] for r in dbq("SELECT name FROM sqlite_master WHERE type='table'")]
rec("sweep_pool_configs" in tbls, "注入串未导致表被 DROP（表仍在）", f"表数={len(tbls)}")
rows = dbq("SELECT strategy FROM sweep_pool_configs WHERE strategy=?", (SQLINJ,))
rec(bool(rows) and rows[0][0] == SQLINJ, "注入串原样存储（参数化查询，非字符串拼接）",
    f"实得={rows[0][0][:40] if rows else None!r}")

# A5 注入进查询串（路径参数注入面）
st, b, _ = http("GET", "/api/research/sweep-pools?strategy=" +
                urllib.request.quote(SQLINJ), token=ADMIN)
rec(st in (200, 400), "查询串注入不引发 5xx", f"http={st}")
tbls2 = [r[0] for r in dbq("SELECT name FROM sqlite_master WHERE type='table'")]
rec(len(tbls2) == len(tbls), "查询串注入后表数量不变", f"{len(tbls)} vs {len(tbls2)}")

# A6 清理并断言残留 0（隔离数据，不污染种子）
c = sqlite3.connect(DB, timeout=10)
c.execute("DELETE FROM sweep_pool_configs WHERE strategy LIKE ?", (MARK + "%",))
c.commit()
left = c.execute("SELECT COUNT(*) FROM sweep_pool_configs WHERE strategy LIKE ?",
                 (MARK + "%",)).fetchone()[0]
c.close()
rec(left == 0, "测试数据已清理，IBV2 残留 0 行", f"残留={left}")

# ── B. 账号隔离 ────────────────────────────────────────────────────────────
# 注：匿名用例必须显式传 token=None —— put_pool 的默认值是 ADMIN，
# 2026-10-12 首轮我在这里漏传，拿到 http=200 是我的脚手架错误（修正前不计缺陷）。
print("\n[B] 账号隔离与权限拒绝语义")
st, _, _ = put_pool(MARK + "-iso", token=TESTER)
rec(st == 403, "低权（无 research_approve）PUT sweep-pools → 403", f"http={st}")
st, _, _ = http("GET", "/api/research/sweep-pools", token=TESTER)
rec(st == 403, "低权 GET sweep-pools → 403", f"http={st}")
st, _, _ = http("GET", "/api/research/sweep-pools")
rec(st == 401, "匿名 GET sweep-pools → 401", f"http={st}")
st, _, _ = put_pool(MARK + "-anon", token=None)
rec(st == 401, "匿名 PUT sweep-pools → 401", f"http={st}")

# ── C. 会话安全 ────────────────────────────────────────────────────────────
print("\n[C] 会话安全")
ONEOFF = login(TESTER_U, TESTER_P)
st_lo, _, _ = http("POST", "/api/auth/logout", token=ONEOFF, raw_body="{}")
st_after, _, _ = http("GET", "/api/research/sweep-pools", token=ONEOFF)
rec(st_lo == 200 and st_after == 401,
    "登出后服务端会话立即失效（防令牌回放）", f"logout={st_lo} 之后={st_after}")
st, _, _ = http("GET", "/api/auth/me", token="forged-" + "a" * 56)
rec(st == 401, "伪造令牌 → 401", f"http={st}")
st, _, _ = http("GET", "/api/auth/me", token=ADMIN[:-1] + ("0" if ADMIN[-1] != "0" else "1"))
rec(st == 401, "改动一位的令牌 → 401（非前缀匹配）", f"http={st}")

# ── D. 错误信封统一性（字节级）─────────────────────────────────────────────
print("\n[D] 错误响应统一信封（字节级 JSON 结构）")
cases = [
    ("匿名访问受保护资源", "GET", "/api/dashboard", None, None, 401),
    ("坏令牌", "GET", "/api/dashboard", "bad-" + "a" * 40, None, 401),
    ("低权访问 admin 端点", "GET", "/api/admin/users", TESTER, None, 403),
    ("未知 /api 路径", "GET", "/api/nonexistent-ibv2-xyz", ADMIN, None, 404),
    ("非法 JSON 体", "POST", "/api/watchlist", ADMIN, None, 400),
    ("缺必填字段", "PUT", "/api/research/sweep-pools", ADMIN, None, 400),
]
env_ok = env_tot = 0
for name, m, p, tok, rb, want in cases:
    if name == "非法 JSON 体":
        st, b, _ = http(m, p, token=tok, raw_body="{not-json")
    elif name == "缺必填字段":
        st, b, _ = http(m, p, token=tok, raw_body=json.dumps({"tp_from": 1}))
    else:
        st, b, _ = http(m, p, token=tok, raw_body=rb)
    if st != want:
        rec(False, f"{name} → 期望 {want}", f"实得 http={st}")
        continue
    rec(True, f"{name} → {want}", "")
    if 400 <= st < 500:
        env_tot += 1
        try:
            j = json.loads(b.decode("utf-8"))
            if isinstance(j, dict) and ("error" in j or "code" in j):
                env_ok += 1
            else:
                rec(False, f"{name} 错误体非统一信封", str(j)[:100])
        except Exception:
            rec(False, f"{name} 错误体非 JSON", b[:100].decode("utf-8", "replace"))
rec(env_tot == 0 or env_ok == env_tot, "客户端错误响应 100% 统一 JSON 信封",
    f"{env_ok}/{env_tot}")

# 413：全局 64KB body 上限
big = '{"strategy":"' + MARK + '-big","note":"' + "x" * 70000 + '"}'
st, b, _ = http("PUT", "/api/research/sweep-pools", token=ADMIN, raw_body=big)
rec(st == 413, "超大请求体 → 413（全局 64KB 闸生效）", f"http={st} {b[:60]}")

# ── E. 幂等 ────────────────────────────────────────────────────────────────
print("\n[E] 幂等与确定性")
p1 = dict(BASE_POOL); p1["strategy"] = MARK + "-idem"
p2 = dict(BASE_POOL); p2["strategy"] = MARK + "-idem"
s1, b1, _ = http("PUT", "/api/research/sweep-pools", token=ADMIN, body=p1)
s2, b2, _ = http("PUT", "/api/research/sweep-pools", token=ADMIN, body=p2)
try:
    j1, j2 = json.loads(b1.decode()), json.loads(b2.decode())
    rec(s1 == 200 and s2 == 200 and j1.get("combos") == j2.get("combos"),
        "同配置两次 PUT 的 combos 一致（幂等写入）", f"{j1.get('combos')} vs {j2.get('combos')}")
except Exception as e:
    rec(False, "幂等 PUT 解析", str(e))
rows = dbq("SELECT COUNT(*) FROM sweep_pool_configs WHERE strategy=?", (MARK + "-idem",))
rec(rows[0][0] == 1, "同 strategy 两次 PUT 只落一行（ON CONFLICT upsert）", f"行数={rows[0][0]}")
c = sqlite3.connect(DB, timeout=10)
c.execute("DELETE FROM sweep_pool_configs WHERE strategy LIKE ?", (MARK + "%",))
c.commit(); c.close()

# ── F. FE↔BE 契约：前端 API 路径 vs 后端路由 ──────────────────────────────
print("\n[F] FE↔BE 契约：前端调用路径 vs 后端注册路由（独立扫描）")
golden = json.load(open(os.path.join(ROOT, "internal/server/route_perms.json"), encoding="utf-8"))
backend_paths = set()
for k in golden:
    parts = k.split(" ", 1)
    backend_paths.add(parts[1] if len(parts) == 2 else parts[0])

# 独立扫前端源码里的 API 路径字面量（不借项目任何工具）
import re
fe_paths = set()
pat = re.compile(r"""['"`](/api/[A-Za-z0-9_\-/{}\$\.]*)['"`]""")
srcdir = os.path.join(ROOT, "web", "src")
for dp, dn, fn in os.walk(srcdir):
    if "__tests__" in dp:
        continue
    for f in fn:
        if not f.endswith((".js", ".jsx")):
            continue
        try:
            txt = open(os.path.join(dp, f), encoding="utf-8", errors="replace").read()
        except Exception:
            continue
        for m in pat.finditer(txt):
            fe_paths.add(m.group(1))
print(f"  前端 API 路径字面量 = {len(fe_paths)}；后端 golden 路由 = {len(backend_paths)}")


def norm(p):
    p = p.split("?")[0]
    p = re.sub(r"\{[^}]*\}", "{}", p)
    p = re.sub(r"\$\{[^}]*\}", "{}", p)
    return p.rstrip("/") or "/"


be_norm = {norm(p) for p in backend_paths}
miss = []
for p in sorted(fe_paths):
    n = norm(p)
    if n in be_norm:
        continue
    # 允许前缀匹配（后端注册的是带 {id} 的模板）
    if any(b.startswith(n + "/") or n.startswith(b + "/") for b in be_norm):
        continue
    miss.append(p)
rec(len(miss) == 0, "前端 API 路径全部能在后端路由表中找到（零断链）",
    f"断链 {len(miss)} 条" + ("：" + ", ".join(miss[:8]) if miss else ""))
for m in miss[:10]:
    print(f"      断链: {m}")

# ── G. FE↔BE 运行期：vite 代理 ─────────────────────────────────────────────
print("\n[G] FE↔BE 运行期（vite dev 代理）")
st, b, h = http("GET", "/api/auth/me", token=ADMIN, host=FRONT)
rec(st == 200, "经 vite 代理访问 /api/auth/me → 200", f"http={st}")
st, b, h = http("GET", "/api/nonexistent-via-vite-ibv2", host=FRONT)
rec(st == 404, "经 vite 代理的未知 /api 路径 → 404（不被 SPA 吞成 HTML）", f"http={st}")
st, b, h = http("GET", "/some/deep/spa/route", host=FRONT, accept="text/html")
ct = h.get("Content-Type", "")
rec(st == 200 and "html" in ct, "前端深路由回落 SPA（200 + HTML）", f"http={st} ct={ct}")

# 构建产物鲜度
dist_idx = os.path.join(ROOT, "web/dist/index.html")
if os.path.exists(dist_idx):
    dmt = os.path.getmtime(dist_idx)
    newest = 0
    newest_f = ""
    for dp, dn, fn in os.walk(os.path.join(ROOT, "web/src")):
        for f in fn:
            if f.endswith((".js", ".jsx", ".css")):
                mt = os.path.getmtime(os.path.join(dp, f))
                if mt > newest:
                    newest, newest_f = mt, os.path.join(dp, f)
    if dmt >= newest:
        rec(True, "web/dist 不早于最新源码（无版本漂移）",
            f"dist={time.strftime('%m-%d %H:%M', time.localtime(dmt))}")
    else:
        warn("web/dist 落后于源码",
             f"dist={time.strftime('%m-%d %H:%M', time.localtime(dmt))} < "
             f"src={time.strftime('%m-%d %H:%M', time.localtime(newest))} ({newest_f})；"
             f"部署脚本部署前无条件 npm run build，生产不受影响")
else:
    warn("web/dist 缺失", "dev 模式运行，跳过鲜度对拍")

# ── H. 本轮施工批（§FIX1012-M5/M8/M2/M6/M10）静态字节级保证 ──────────────────
# 与运行期注入面不同，这一组校验「本次施工落地的代码不变量」——不依赖运行服务，纯读源码/磁盘。
# 覆盖：M8 业务路由全套 ProtectedRoute 对账门 / M5 auth:expired 下沉 hook 且 App 壳用例不再 import App
# / M2 开闸检查单存在且可执行 / M6 载荷契约 golden 存在且条数充足+测试存在 / M10 生产 console 收敛+max-lines 门。
print("\n── H. §FIX1012 施工批静态保证（M5/M8/M2/M6/M10）────────────────────")

import re as _re


def _read(p):
    try:
        with open(os.path.join(ROOT, p), encoding="utf-8") as f:
            return f.read()
    except Exception:
        return None


def _count(s, sub):
    return s.count(sub) if s is not None else -1


# M8：业务路由全部套 ProtectedRoute checked={meChecked}（纵深对账门）
appjsx = _read("web/src/App.jsx")
if appjsx is None:
    rec(False, "M8 App.jsx 可读", "文件缺失")
else:
    routes = _count(appjsx, "<Route ")
    guarded = _count(appjsx, "element={<ProtectedRoute")
    bare = routes - guarded
    rec(bare <= 2, "M8 业务路由全部套 ProtectedRoute（裸 Route ≤2，仅剩 / 重定向与 /403）",
        f"裸={bare} 总={routes} 套={guarded}")
    # 负向：存在不带 checked 的 ProtectedRoute 即退化（§A5 对账门被绕过）
    bad = _re.findall(r"<ProtectedRoute\b(?![^>]*checked=\{meChecked\})", appjsx)
    rec(len(bad) == 0, "M8 不存在不带 checked={meChecked} 的 ProtectedRoute（§A5 对账门不被绕过）",
        f"退化数={len(bad)}")

# M5：auth:expired 监听下沉为独立 hook；
# 粒度纪律：hook 级用例（use_auth_expired.test.jsx）不得 import 整个 App（保持 <2s 单元粒度，门禁不变脆）；
# 集成锚（auth_expired_h7.test.jsx）允许 import App（它本就是端到端锚，只进全量 npm test）。
hook = _read("web/src/hooks/useAuthExpired.js")
rec(hook is not None and "useAuthExpired" in hook, "M5 useAuthExpired hook 已落地（web/src/hooks/useAuthExpired.js）")
hk = _read("web/src/__tests__/use_auth_expired.test.jsx")
if hk is None:
    rec(False, "M5 hook 级用例可读", "use_auth_expired.test.jsx 缺失")
else:
    rec("../App.jsx" not in hk and "import('../App" not in hk,
        "M5 hook 级用例不 import 整个 App（粒度下沉，门禁不变脆）")
anchor = _read("web/src/__tests__/auth_expired_h7.test.jsx")
rec(anchor is not None and "import('../App.jsx')" in anchor,
    "M5 App 壳集成锚保留（auth_expired_h7 仍 import App 作端到端锚，只进全量）")

# M2：开闸检查单存在且可执行（fail-safe 双市场闸只可经检查单灰度开启）
gate = os.path.join(ROOT, "scripts/preflight_open_gate.sh")
rec(os.path.isfile(gate), "M2 开闸检查单存在（scripts/preflight_open_gate.sh）")
rec(os.path.isfile(gate) and os.access(gate, os.X_OK), "M2 开闸检查单可执行（chmod +x）")

# M6：FE↔BE 载荷契约 golden 存在且条数充足 + 对应 golden 测试存在
ep = _read("internal/server/endpoint_payloads.json")
ep_ok = False
ep_n = 0
if ep:
    try:
        ep_n = len(json.loads(ep))
        ep_ok = ep_n >= 50
    except Exception:
        ep_ok = False
rec(ep_ok, f"M6 载荷契约 golden 存在且 ≥50 条（防空册子骗绿，实有 {ep_n}）")
ept = _read("internal/server/endpoint_payloads_test.go")
rec(ept is not None and "TestEndpointPayloadsGolden" in ept, "M6 载荷契约 golden 测试 TestEndpointPayloadsGolden 存在")

# M10：生产代码 console 收敛到 logger + eslint max-lines 门（god component 失控有闸）
research = _read("web/src/pages/Research.jsx")
if research is None:
    rec(False, "M10 Research.jsx 可读", "文件缺失")
else:
    rec("console." not in research, "M10 Research.jsx 无 console. 残留（生产日志收口到 logger）")
eslint = _read("web/eslint.config.js")
rec(eslint is not None and "'max-lines'" in eslint, "M10 eslint 已启用 max-lines 规则（god component 失控有门）")

# ── 汇总 ──────────────────────────────────────────────────────────────────
print("\n" + "=" * 78)
print(f"汇总: PASS={len(PASS)}  FAIL={len(FAIL)}  WARN={len(WARN)}")
if FAIL:
    print("失败项:")
    for n, d in FAIL:
        print(f"  - {n} :: {d}")
print("=" * 78)
sys.exit(1 if FAIL else 0)
