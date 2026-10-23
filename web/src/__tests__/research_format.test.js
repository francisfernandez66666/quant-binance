// ── §AUDIT1019-M8 尾巴：纯格式化/派生函数单测 ──
// 覆盖从 Research.jsx 抽出的 16 个零组件状态依赖函数（research_format.js）。
// 断言口径与抽出前逐字节等价：中文文案、空值占位符 '-'、阈值 0.3、fmt 4 位小数等一律不动。
// 这些函数不依赖 useState/useRef，故可独立单测，无需挂载整页。
import { describe, it, expect } from 'vitest'
import {
  fmtPctGlobal, pct, fmtRows, fmt, signClass, signColor, fmtNum,
  weightList, depthSummary, kindLabel, factorDirs, parseReason,
  fmtPct, verdict, plainLines, btTested,
} from '../pages/research_format.js'

describe('fmtPctGlobal 全局百分比（2 位）', () => {
  it('空值返回占位符 -', () => {
    expect(fmtPctGlobal(null)).toBe('-')
    expect(fmtPctGlobal(undefined)).toBe('-')
    expect(fmtPctGlobal(NaN)).toBe('-')
  })
  it('正常数值保留两位小数并加 %', () => {
    expect(fmtPctGlobal(12.345)).toBe('12.35%')
    expect(fmtPctGlobal(0)).toBe('0.00%')
  })
})

describe('pct 百分比整数（0~100 夹取）', () => {
  it('空值返回 0', () => {
    expect(pct(null)).toBe(0)
    expect(pct(undefined)).toBe(0)
    expect(pct(NaN)).toBe(0)
  })
  it('四舍五入并夹取上限 100', () => {
    expect(pct(0.3456)).toBe(35)
    expect(pct(1.5)).toBe(100)
    expect(pct(0.9999)).toBe(100)
  })
})

describe('fmtRows 千分位计数', () => {
  it('空值返回 -', () => {
    expect(fmtRows(null)).toBe('-')
    expect(fmtRows(undefined)).toBe('-')
    expect(fmtRows(NaN)).toBe('-')
  })
  it('整数千分位', () => {
    expect(fmtRows(1234567)).toBe('1,234,567')
  })
})

describe('fmt 数值（4 位小数）', () => {
  it('空值返回 -', () => {
    expect(fmt(null)).toBe('-')
    expect(fmt(NaN)).toBe('-')
  })
  it('保留 4 位小数', () => {
    expect(fmt(0.123456)).toBe('0.1235')
    expect(fmt(-0.1)).toBe('-0.1000')
  })
})

describe('signClass 盈亏样式类', () => {
  it('非有限值返回空串', () => {
    expect(signClass(null)).toBe('')
    expect(signClass(NaN)).toBe('')
  })
  it('正负映射 pos/neg', () => {
    expect(signClass(0)).toBe('pos')
    expect(signClass(1.2)).toBe('pos')
    expect(signClass(-0.5)).toBe('neg')
  })
})

describe('signColor 红涨绿跌颜色', () => {
  it('非有限值返回弱色', () => {
    expect(signColor(undefined)).toBe('var(--app-faint)')
  })
  it('正红负绿', () => {
    expect(signColor(3)).toBe('var(--app-up)')
    expect(signColor(-3)).toBe('var(--app-down)')
  })
})

describe('fmtNum 指定小数位', () => {
  it('空值返回 -', () => {
    expect(fmtNum(null)).toBe('-')
    expect(fmtNum(NaN)).toBe('-')
  })
  it('默认 0 位、可指定位数', () => {
    expect(fmtNum(5)).toBe('5')
    expect(fmtNum(5.678, 2)).toBe('5.68')
  })
})

describe('weightList 战法权重解析', () => {
  it('嵌套 weights 对象排序（降序）', () => {
    const c = { weights: '{"weights":{"f1":0.6,"f2":0.4},"directions":{"f1":1}}' }
    expect(weightList(c)).toEqual([['f1', 0.6], ['f2', 0.4]])
  })
  it('平铺权重对象（无 .weights）直接取', () => {
    const c = { weights: '{"f1":0.9,"f2":0.1}' }
    expect(weightList(c)).toEqual([['f1', 0.9], ['f2', 0.1]])
  })
  it('过滤非数值项', () => {
    const c = { weights: '{"f1":0.6,"f2":"x"}' }
    expect(weightList(c)).toEqual([['f1', 0.6]])
  })
  it('损坏 JSON 或空返回 []', () => {
    expect(weightList({ weights: '{bad' })).toEqual([])
    expect(weightList({})).toEqual([])
  })
})

describe('depthSummary 盘口深度汇总', () => {
  it('解析 weights 为对象', () => {
    expect(depthSummary({ weights: '{"a":1}' })).toEqual({ a: 1 })
  })
  it('损坏 JSON 返回 {}', () => {
    expect(depthSummary({ weights: 'xxx' })).toEqual({})
  })
})

describe('kindLabel 研究类型中文标签', () => {
  it('映射已知类型', () => {
    expect(kindLabel('factor')).toBe('因子战法')
    expect(kindLabel('pattern')).toBe('形态战法')
    expect(kindLabel('weights')).toBe('权重优化')
    expect(kindLabel('depth')).toBe('盘口扫描')
  })
  it('未知类型原样透传', () => {
    expect(kindLabel('unknown')).toBe('unknown')
  })
})

describe('factorDirs 因子方向解析', () => {
  it('读取 weights.directions', () => {
    const c = { weights: '{"directions":{"f1":1,"f2":-1}}' }
    expect(factorDirs(c)).toEqual({ f1: 1, f2: -1 })
  })
  it('缺失 directions 或坏 JSON 返回 {}', () => {
    expect(factorDirs({ weights: '{"a":1}' })).toEqual({})
    expect(factorDirs({ weights: 'bad' })).toEqual({})
    expect(factorDirs({})).toEqual({})
  })
})

describe('parseReason 命中原因提取', () => {
  it('提取样本内/样本外 IR 与反推超额', () => {
    const c = { reason: '样本内IR=0.52;样本外IR=0.41;反推超额=0.07' }
    expect(parseReason(c, '样本内IR')).toBeCloseTo(0.52)
    expect(parseReason(c, '样本外IR')).toBeCloseTo(0.41)
    expect(parseReason(c, '反推超额')).toBeCloseTo(0.07)
  })
  it('缺失字段返回 null', () => {
    expect(parseReason({ reason: '样本内IR=0.5' }, '样本外IR')).toBeNull()
  })
})

describe('fmtPct 0~1 比率百分比', () => {
  it('空值返回 -', () => {
    expect(fmtPct(null)).toBe('-')
    expect(fmtPct(NaN)).toBe('-')
  })
  it('1 位小数并带 + 号（非负）', () => {
    expect(fmtPct(0.07)).toBe('+7.0%')
    expect(fmtPct(-0.1)).toBe('-10.0%')
  })
})

describe('verdict 回测结论判定（阈值 0.3）', () => {
  const pass = { reason: '样本内IR=0.52;样本外IR=0.41;反推超额=0.07' }
  it('三项达标 → ok 真（通过文案）', () => {
    const r = verdict(pass)
    expect(r.ok).toBe(true)
    expect(r.text).toContain('不是碰运气')
  })
  it('样本内 IR 低于 0.3 → 失败', () => {
    const r = verdict({ reason: '样本内IR=0.2;样本外IR=0.41;反推超额=0.07' })
    expect(r.ok).toBe(false)
    expect(r.text).toContain('不建议直接拿来实盘')
  })
  it('反推超额为负 → 失败', () => {
    const r = verdict({ reason: '样本内IR=0.5;样本外IR=0.5;反推超额=-0.1' })
    expect(r.ok).toBe(false)
  })
  it('无 reason 字段 → 缺省按通过（全部 null 视为达标）', () => {
    expect(verdict({}).ok).toBe(true)
  })
})

describe('plainLines 候选理由纯文本行', () => {
  it('仅有样本内 IR → 一行含稳定度与「前半段」', () => {
    const lines = plainLines({ reason: '样本内IR=0.52' })
    expect(lines).toHaveLength(1)
    expect(lines[0]).toContain('前半段')
    expect(lines[0]).toContain('0.5200') // fmt 4 位
  })
  it('含反推超额 → 含「多赚」与 +7.0%', () => {
    const lines = plainLines({ reason: '反推超额=0.07' })
    expect(lines[0]).toContain('多赚')
    expect(lines[0]).toContain('+7.0%')
  })
  it('三条皆无且 reason 含「通过护栏」→ 通过引导语', () => {
    expect(plainLines({ reason: '通过护栏测试' })).toEqual(['电脑验证通过，这条规律可以试试。'])
  })
  it('三条皆无且不含护栏 → 未通过文案', () => {
    expect(plainLines({ reason: '随便写' })).toEqual(['这条规律未通过验证。'])
  })
})

describe('btTested 是否已回测', () => {
  it('布尔 backtest_done 优先', () => {
    expect(btTested({ backtest_done: true })).toBe(true)
    expect(btTested({ backtest_done: false })).toBe(false)
  })
  it('按 avg_excess 非零判断（undefined 也视为 !==0 → true，与抽出前一致）', () => {
    expect(btTested({ avg_excess: 0.5 })).toBe(true)
    expect(btTested({ avg_excess: 0 })).toBe(false)
    expect(btTested({})).toBe(true)
  })
})
