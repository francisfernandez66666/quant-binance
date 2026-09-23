#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# 美股历史日线下载器（§BINANCE-P4，PLAN Phase 4 第 2 条 / RESEARCH_US_DATA_STRATEGY §3.1）：
# 币安 Stocks API 无历史行情端点，回测历史走 Alpaca `GET /v2/stocks/bars`（免费档 7+ 年、
# IEX feed、200 req/min），统一落研究库 `daily` 表（ts_code=裸 ticker，如 AAPL），
# 与 A 股/加密同表同形状，btreplay/回测引擎零改动复用。
#
# 契约要点（RESEARCH_US_DATA §3.1，逐参数对齐）：
#   · 认证头 APCA-API-KEY-ID / APCA-API-SECRET-KEY，凭证走环境变量，不落盘不入库；
#   · limit 是跨全部 symbol 的总数据点数（≤10000）→ 多票/长区间必须按 page_token 翻页；
#   · adjustment=all（split/dividend/spin-off 复权），feed 缺省免费档 iex（可 --feed sip）；
#   · 429 限速：读 X-RateLimit-Reset 退避后重试。
# 退出码口径（2026-09-23 假绿闸收口）：未配置凭证=指引后退出码 0（夜间批缺数据源不炸链）；
# 有凭证但任一批失败或 0 根入库=退出码 1（401/资格未开通这类配置错必须炸调度链）。
#
# 用法：ALPACA_API_KEY_ID=xx ALPACA_API_SECRET_KEY=yy \
#       python3 scripts/download_alpaca_bars.py <trading.db> AAPL TSLA --start 2024-01-01 --end 2024-12-31
import argparse
import json
import os
import sqlite3
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

HOST = "https://data.alpaca.markets"
BATCH = 8  # 每次请求的 symbols 数（limit=10000 总点数预算下，日线一年一票约 250 点）


def api_get(path, params, key_id, key_secret):
    """一次 GET：带认证头与查询串，返回 (status, headers, body_json_or_None)；429 由调用处退避。"""
    url = HOST + path + "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(url, headers={
        "APCA-API-KEY-ID": key_id,
        "APCA-API-SECRET-KEY": key_secret,
        "Accept": "application/json",
    })
    try:
        with urllib.request.urlopen(req, timeout=60) as r:
            return r.status, dict(r.headers), json.loads(r.read().decode())
    except urllib.error.HTTPError as e:
        body = None
        try:
            body = json.loads(e.read().decode())
        except Exception:
            pass
        return e.code, dict(e.headers or {}), body


def fetch_bars(symbols, args, key_id, key_secret):
    """按 symbols 分批拉日线；处理 page_token 翻页与 429 退避（每页最多重试 10 次，
    防毒循环）。返回 (bars_by_symbol, failed)——failed=True 表示任一批次彻底失败，
    调用方据此非零退出（有凭证却全批 401/404 绝不能"共 0 根"绿脸收工，假绿闸同族）。"""
    out = {}
    failed = False
    for i in range(0, len(symbols), BATCH):
        batch = symbols[i:i + BATCH]
        page = ""
        retries = 0
        while True:
            params = {
                "symbols": ",".join(batch),
                "timeframe": "1Day",
                "adjustment": "all",
                "feed": args.feed,
                "limit": 10000,
            }
            if args.start:
                params["start"] = args.start
            if args.end:
                params["end"] = args.end
            if page:
                params["page_token"] = page
            status, headers, body = api_get("/v2/stocks/bars", params, key_id, key_secret)
            if status == 429:
                retries += 1
                if retries > 10:  # 限速重试有界：持续 429=配额真耗尽，让本批带错收口而不是永挂
                    print(f"  !! 429 重试 10 次仍限流，弃批: {batch}", file=sys.stderr)
                    failed = True
                    break
                wait = int(headers.get("X-RateLimit-Reset", "60") or 60)
                wait = min(max(wait, 5), 120)
                print(f"  429 限速，退避 {wait}s 后重试本批: {batch}")
                time.sleep(wait)
                continue
            if status != 200 or not body:
                print(f"  !! /v2/stocks/bars HTTP {status}: {body}", file=sys.stderr)
                failed = True
                break
            for sym, bars in (body.get("bars") or {}).items():
                out.setdefault(sym.upper(), []).extend(bars or [])
            page = body.get("next_page_token") or ""
            retries = 0  # 翻页成功即重置本批重试预算
            if not page:
                break
        time.sleep(0.3)  # 200 req/min 免费档：批间轻节流
    return out, failed


def upsert(conn, symbol, bars):
    """Alpaca bar {t,o,h,l,c,v,n,vw} → daily 表行；trade_date 取 RFC-3339 的 UTC 日期段。"""
    rows = []
    prev_close = None
    for b in sorted(bars, key=lambda x: x.get("t", "")):
        day = str(b.get("t", ""))[:10].replace("-", "")
        if len(day) != 8:
            continue
        o, h, l, c, v = (float(b.get(k, 0) or 0) for k in ("o", "h", "l", "c", "v"))
        amount = c * v  # 免费档无逐笔成交额，用收盘×量近似（回测只吃 OHLC+vol，口径备注）
        pc = prev_close if prev_close else c
        chg = c - pc
        pct = chg * 100.0 / pc if pc else 0.0
        rows.append((symbol, day, o, h, l, c, pc, chg, pct, v, amount))
        prev_close = c
    conn.executemany(
        "INSERT OR REPLACE INTO daily (ts_code, trade_date, open, high, low, close, pre_close, change, pct_chg, vol, amount)"
        " VALUES (?,?,?,?,?,?,?,?,?,?,?)", rows)
    return len(rows)


def main():
    ap = argparse.ArgumentParser(description="Alpaca 美股历史日线下载入库（回测数据管道·路线A 数据源）")
    ap.add_argument("db", nargs="?", default=os.path.expanduser("~/.quant-trading-v2/trading.db"))
    ap.add_argument("symbols", nargs="*", help="裸 ticker，如 AAPL BRK.B（含点分代码原样入库，market 由尾点判定）")
    ap.add_argument("--start", default="", help="RFC-3339 或 YYYY-MM-DD")
    ap.add_argument("--end", default="", help="同上；免费档 SIP 的 end 须早于当前 15 分钟")
    ap.add_argument("--feed", default="iex", choices=["iex", "sip"], help="缺省 iex（免费档）")
    args = ap.parse_args()

    key_id = os.environ.get("ALPACA_API_KEY_ID", "").strip()
    key_secret = os.environ.get("ALPACA_API_SECRET_KEY", "").strip()
    if not key_id or not key_secret:
        # fail-soft：缺凭证只提示不报错退出——夜间调度里该步缺席不影响其余数据链路
        print("跳过：未配置 ALPACA_API_KEY_ID / ALPACA_API_SECRET_KEY 环境变量。\n"
              "获取：https://app.alpaca.markets → Data Credentials（免费档 200 req/min、7+ 年历史）。")
        sys.exit(0)
    if not args.symbols:
        print("无 symbols 参数，无需下载。")
        sys.exit(0)
    conn = sqlite3.connect(args.db)
    conn.execute("PRAGMA journal_mode=WAL")
    data, failed = fetch_bars([s.upper() for s in args.symbols], args, key_id, key_secret)
    total = 0
    for sym, bars in data.items():
        n = upsert(conn, sym, bars)
        total += n
        print(f"{sym}: {n} 根入库")
    conn.commit()
    conn.close()
    print(f"完成：共 {total} 根美股日线入库 → {args.db}")
    # 假绿闸：凭证在握却 0 根/任一批失败 → 非零退出（401/资格未开通族必须炸调度链，
    # 与"无凭证 fail-soft 早退"是两个明确分开的口径）。
    if failed or total == 0:
        print("!! 本轮存在失败批次或未取回任何数据（见上方错误行），退出码 1", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
