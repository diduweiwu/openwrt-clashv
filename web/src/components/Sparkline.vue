<script setup>
// 手绘 canvas 流量曲线（无第三方图表库，保持小体积低内存）
// 固定显示最近 10 分钟窗口（刻度 1 分钟一格）：历史由后端每秒采样并落盘持久化（/api/traffic/history
// 首屏灌入），刷新页面/重启插件曲线不清零；X 轴按真实时间定位（整分钟对齐刻度），
// 内核停止/页面关闭期没有采样，曲线按时间断档分段绘制、不跨档连线；悬浮出十字线
// + 读数卡显示所指时刻的上传/下载速率，贴右侧自动向左翻转，交互对齐 Clash Verge 的流量图
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { fmtRate } from '../store.js'
import { resolvedTheme } from '../theme.js'

const props = defineProps({
  series: { type: Array, default: () => [] }, // [{up, down, t?}]；t 为采样时刻毫秒（旧数据无 t，按 1 秒采样间隔倒推）
  height: { type: Number, default: 120 },
})

const canvas = ref(null)
const tip = ref(null)    // 悬浮读数卡内容 {left, flip, time, up, down}；null 隐藏
const hoverX = ref(null) // 悬浮点横坐标（CSS 像素，相对 canvas 左缘）；null 表示未悬浮

const PAD_T = 8   // 绘图区上边距
const PAD_R = 10  // 绘图区右边距
const PAD_B = 20  // 绘图区下边距（留给时间刻度）
const WIN_MS = 10 * 60 * 1000 // 显示窗口：横轴全程固定 10 分钟（后端保留 1 小时历史，这里只展示最近 10 分钟）
const SAMPLE_MS = 1000        // 采样间隔：后端 1 秒一采样，旧点无 t 时倒推用
const MAX_POINTS = 3600       // 最多取 1 小时 × 1 秒的采样（切片后仅 WIN_MS 窗口内会绘出）
const GAP_MS = 5000           // 相邻采样间隔超过此值视为断档（内核停止/页面关闭期），曲线分段绘制
let ro = null                 // ResizeObserver：容器宽度变化（侧栏收起/转屏）时重绘

/**
 * 把 Y 轴满程值向上取整到整洁刻度，让满程尽量贴近当前峰值、减少轴顶留白。
 * 刻度序列为 0.5/0.75/1/1.5/2/3/4/5/6/8/10 × 基数（1024 的幂，8KB/s 起步），
 * 相邻档密度约 25%，比纯 2 的幂（相邻翻倍、最多浪费一半高度）贴合得多；
 * 标签仍是整洁速率：512 KB/s、768 KB/s、1.0 MB/s、1.5 MB/s…
 * 注意不能只在单一数量级内取「1/2/4 倍」：数据可能达基数的数百倍
 * （如 576 KB/s = 576×1KB），那样取出的满程反而小于数据、曲线顶出绘图区。
 *
 * @param {number} v 待取整的速率下界（字节/秒），恒返回 >= max(v, 8KB)
 * @returns {number} 取整后的满程值
 *
 * @example
 * niceCeil(589_824) // → 655_360（640 KB/s，而非旧逻辑的 1 MB/s）
 */
function niceCeil(v) {
  const steps = [0.5, 0.75, 1, 1.5, 2, 3, 4, 5, 6, 8, 10]
  let unit = 8 * 1024
  if (v <= unit) return unit
  // 跨数量级：当前基数 ×10 仍盖不住时基数 ×16（8K→128K→2M…，相邻基数区间重叠无空档）
  while (unit * steps[steps.length - 1] < v) unit *= 16
  for (const k of steps) {
    const m = unit * k
    if (m >= v) return m
  }
  return unit * steps[steps.length - 1]
}

/**
 * 毫秒时间戳 → HH:MM:SS 文案，用于悬浮读数卡。
 *
 * @param {number} ms 毫秒时间戳
 * @returns {string} 如 "08:19:41"
 *
 * @example
 * fmtClock(Date.now()) // → "14:03:27"
 */
function fmtClock(ms) {
  const d = new Date(ms)
  const p = (x) => String(x).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

/**
 * 毫秒时间戳 → HH:MM 文案，用于 X 轴分钟刻度（秒位恒为 0，不显示）。
 *
 * @param {number} ms 毫秒时间戳
 * @returns {string} 如 "08:19"
 *
 * @example
 * fmtHM(Date.now()) // → "14:03"
 */
function fmtHM(ms) {
  const d = new Date(ms)
  const p = (x) => String(x).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}`
}

/**
 * 读取当前主题下画布要用的颜色（CSS 变量实时取值，深浅主题与切换即时生效）。
 *
 * @returns {{dim: string, border: string, dotBg: string}} 轴文字色 / 网格线色 / 悬浮圆点底色
 *
 * @example
 * const theme = readTheme() // → { dim: '#8b91a0', ... }
 */
function readTheme() {
  const css = getComputedStyle(document.documentElement)
  return {
    dim: css.getPropertyValue('--text-dim').trim() || '#888',
    border: css.getPropertyValue('--border').trim() || 'rgba(128,128,128,.25)',
    dotBg: css.getPropertyValue('--bg-card').trim() || '#fff',
  }
}

/**
 * 选 X 轴刻度步长（分钟）：窗口恒 10 分钟，刻度候选 1/2/5 分钟，
 * 取能满足「每格宽度不小于 48px」的最小步长——默认 1 分钟一格，
 * 窄屏自动放宽到 2/5 分钟，保证标签不叠印。
 *
 * @param {number} plotW 绘图区像素宽
 * @returns {number} 步长（分钟），1/2/5 之一
 *
 * @example
 * pickXStep(984) // → 1（每格约 98px）
 */
function pickXStep(plotW) {
  for (const step of [1, 2, 5]) {
    if ((plotW * step) / (WIN_MS / 60000) >= 48) return step
  }
  return 5
}

/**
 * 准备一帧绘制所需的全部上下文：画布尺寸/DPR、数据、坐标映射与时间取值。
 * 数据不足两点时清掉悬浮卡并返回 null，调用方据此跳过后续绘制。
 *
 * X 轴是时间轴：X(t) 按真实时刻在 [left, right] 窗口内线性定位，
 * 历史不足 10 分钟时曲线只占窗口右侧，左侧留白等待采样填满。
 *
 * @returns {object|null} 场景对象 {ctx,w,h,data,n,max,theme,px0,px1,py0,py1,X,Y,timeAt,left,right}
 *
 * @example
 * const s = buildScene(); if (!s) return; drawAxes(s)
 */
function buildScene() {
  const el = canvas.value
  if (!el) return null
  const dpr = window.devicePixelRatio || 1
  const w = el.clientWidth
  const h = props.height
  el.width = w * dpr
  el.height = h * dpr
  const ctx = el.getContext('2d')
  ctx.scale(dpr, dpr)
  ctx.clearRect(0, 0, w, h)
  ctx.font = '11px -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", sans-serif'

  const data = props.series.slice(-MAX_POINTS)
  const n = data.length
  if (n < 2) { tip.value = null; return null }

  let peak = 1024 * 8 // 满程下限 8 KB/s：全零时曲线贴底而不是除零
  for (const p of data) peak = Math.max(peak, p.up, p.down)
  const max = niceCeil(peak)
  const theme = readTheme()

  // 左侧留白按 Y 轴三档标签中最宽者自适应（右对齐画在留白里，+14px 余量含文字到绘图区间距）
  let px0 = 30
  for (const v of [0, max / 2, max]) px0 = Math.max(px0, ctx.measureText(fmtRate(v)).width + 14)
  const px1 = w - PAD_R, py0 = PAD_T, py1 = h - PAD_B
  // 窗口右端对齐到「包含最新采样点的整分钟」：刻度位置与数字每分钟才跳变一次
  // （连续滚动会每秒微移）；代价是曲线末端与右缘之间有 0~59 秒的对齐空隙，
  // 由 drawSeries 在曲线末端画实时位置点来解释
  const lastMs = data[n - 1].t ?? Date.now()
  const right = Math.ceil(lastMs / 60000) * 60000
  const left = right - WIN_MS
  const X = (t) => px0 + ((t - left) / WIN_MS) * (px1 - px0)
  const Y = (v) => py1 - (v / max) * (py1 - py0)
  const timeAt = (i) => data[i].t ?? (right - (n - 1 - i) * SAMPLE_MS)
  return { ctx, w, h, data, n, max, theme, px0, px1, py0, py1, X, Y, timeAt, left, right }
}

/**
 * 绘制坐标轴：Y 轴三档速率刻度（0/半程/满程）+ 横向网格线；
 * X 轴按整分钟对齐画刻度（步长见 pickXStep）+ 底部小竖线。
 *
 * @param {object} s buildScene 返回的场景对象
 * @returns {void}
 *
 * @example
 * drawAxes(scene)
 */
function drawAxes(s) {
  const { ctx, max, theme, px0, px1, py0, py1, X, Y, left, right } = s
  // Y 轴：右对齐画在留白里，网格线横贯绘图区
  ctx.textAlign = 'right'
  ctx.textBaseline = 'middle'
  for (const v of [0, max / 2, max]) {
    ctx.fillStyle = theme.dim
    ctx.fillText(fmtRate(v), px0 - 6, Y(v))
    ctx.strokeStyle = theme.border
    ctx.beginPath(); ctx.moveTo(px0, Y(v)); ctx.lineTo(px1, Y(v)); ctx.stroke()
  }
  // X 轴：从窗口左端向上取整到整分钟开始，每 step 分钟一个刻度（真实时钟整分对齐）
  const step = pickXStep(px1 - px0)
  const stepMs = step * 60000
  let t = Math.ceil(left / 60000) * 60000
  // 让 t 落在步长对齐的整分上，保证刻度分钟值规律（如每 5 分钟：09:05、09:10…）
  t = Math.ceil(t / stepMs) * stepMs
  for (; t <= right; t += stepMs) {
    const x = X(t)
    ctx.strokeStyle = theme.border
    ctx.beginPath(); ctx.moveTo(x, py1); ctx.lineTo(x, py1 + 3); ctx.stroke()
    ctx.fillStyle = theme.dim
    ctx.textBaseline = 'top'
    // 首尾刻度向内对齐防出界，其余居中
    ctx.textAlign = x < px0 + 30 ? 'left' : x > px1 - 30 ? 'right' : 'center'
    ctx.fillText(fmtHM(t), x, py1 + 5)
  }
}

/**
 * 把采样序列按时间断档切分成绘制段：相邻两点间隔超过 GAP_MS（内核停止、
 * 页面关闭期没有采样）即断开，曲线与面积填充都不跨档连线，避免把「没流量」
 * 画成「流量为零的直线」造成误读。
 *
 * @param {object} s buildScene 返回的场景对象
 * @returns {Array<[number, number]>} 段列表，每段为 [起始下标, 结束下标]（闭区间）
 *
 * @example
 * const segs = splitSegments(scene) // → [[0, 120], [360, 3599]]
 */
function splitSegments(s) {
  const { data, n, timeAt } = s
  const segs = []
  let start = 0
  for (let i = 1; i < n; i++) {
    if (timeAt(i) - timeAt(i - 1) > GAP_MS) {
      segs.push([start, i - 1])
      start = i
    }
  }
  segs.push([start, n - 1])
  return segs
}

/**
 * 绘制上传/下载两条曲线及其渐变面积填充（颜色与上方速率瓦片一一对应）。
 * 点按采样时刻在窗口内定位；断档处（splitSegments）分段绘制不跨档连线。
 *
 * @param {object} s 场景对象
 * @returns {void}
 *
 * @example
 * drawSeries(scene)
 */
function drawSeries(s) {
  const { ctx, data, n, h, py0, py1, X, Y, timeAt } = s
  const segs = splitSegments(s)
  const line = (key, stroke, fill) => {
    for (const [a, b] of segs) {
      ctx.beginPath()
      ctx.moveTo(X(timeAt(a)), Y(data[a][key]))
      for (let i = a + 1; i <= b; i++) ctx.lineTo(X(timeAt(i)), Y(data[i][key]))
      ctx.strokeStyle = stroke
      ctx.lineWidth = 1.8
      ctx.lineJoin = 'round'
      ctx.stroke()
      ctx.lineTo(X(timeAt(b)), py1)
      ctx.lineTo(X(timeAt(a)), py1)
      ctx.closePath()
      const g = ctx.createLinearGradient(0, py0, 0, h)
      g.addColorStop(0, fill)
      g.addColorStop(1, 'transparent')
      ctx.fillStyle = g
      ctx.fill()
    }
  }
  line('down', '#5b6bf0', 'rgba(91, 107, 240, 0.22)')
  line('up', '#3fb96f', 'rgba(63, 185, 111, 0.18)')
  // 曲线末端实时位置点：窗口右缘是整分钟对齐的，末端与右缘之间有 0~59 秒空隙，
  // 两枚小点标出「此刻」的真实读数位置（重叠时绿点覆盖蓝点，与悬浮圆点同规则）
  const xt = X(timeAt(n - 1))
  ctx.beginPath(); ctx.arc(xt, Y(data[n - 1].down), 2.5, 0, Math.PI * 2); ctx.fillStyle = '#5b6bf0'; ctx.fill()
  ctx.beginPath(); ctx.arc(xt, Y(data[n - 1].up), 2.5, 0, Math.PI * 2); ctx.fillStyle = '#3fb96f'; ctx.fill()
}

/**
 * 找悬浮横坐标命中的数据点：按时刻换算后在窗口内取时间最近的一点。
 *
 * @param {object} s 场景对象
 * @returns {number} 命中的点下标；落在无数据留白区（距离超过 2.5 个采样间隔）返回 -1
 *
 * @example
 * const i = hitIndex(scene); if (i < 0) tip.value = null
 */
function hitIndex(s) {
  const { data, n, px0, px1, timeAt, left, right } = s
  const tx = left + ((hoverX.value - px0) / (px1 - px0)) * (right - left)
  const clamped = Math.min(right, Math.max(left, tx))
  let best = -1
  let bestDist = Infinity
  for (let i = 0; i < n; i++) {
    const d = Math.abs(timeAt(i) - clamped)
    if (d < bestDist) { bestDist = d; best = i }
  }
  return bestDist <= SAMPLE_MS * 2.5 ? best : -1
}

/**
 * 绘制悬浮辅助元素并同步读数卡：竖虚线 + 两系列交点圆点 + tip 内容。
 * 十字线吸附到最近数据点；悬浮在窗口左侧无数据留白区时不显示任何辅助元素。
 *
 * @param {object} s 场景对象
 * @returns {void}
 *
 * @example
 * drawHover(scene)
 */
function drawHover(s) {
  const { ctx, data, theme, py0, py1, X, Y, timeAt } = s
  if (hoverX.value == null) { tip.value = null; return }
  const i = hitIndex(s)
  if (i < 0) { tip.value = null; return }
  const { px0, px1 } = s
  const x = X(timeAt(i))
  ctx.setLineDash([4, 3])
  ctx.strokeStyle = theme.dim
  ctx.lineWidth = 1
  ctx.beginPath(); ctx.moveTo(x, py0); ctx.lineTo(x, py1); ctx.stroke()
  ctx.setLineDash([])
  const dot = (v, color) => {
    ctx.beginPath(); ctx.arc(x, Y(v), 3, 0, Math.PI * 2)
    ctx.fillStyle = theme.dotBg; ctx.fill()
    ctx.strokeStyle = color; ctx.lineWidth = 1.6; ctx.stroke()
  }
  dot(data[i].down, '#5b6bf0')
  dot(data[i].up, '#3fb96f')
  tip.value = {
    left: x,
    flip: x > px0 + (px1 - px0) * 0.6, // 贴右侧 40% 区域时读数卡向左翻，防出界
    time: fmtClock(timeAt(i)),
    up: fmtRate(data[i].up),
    down: fmtRate(data[i].down),
  }
}

/** 重绘一帧：场景准备失败（数据不足/组件已卸载）即止 */
function draw() {
  const s = buildScene()
  if (!s) return
  drawAxes(s)
  drawSeries(s)
  drawHover(s)
}

/**
 * 悬浮移动：记录横坐标（相对 canvas 左缘）并立即重绘出十字线与读数卡。
 *
 * @param {PointerEvent} e 指针移动事件
 * @returns {void}
 *
 * @example
 * <canvas @pointermove="onMove">
 */
function onMove(e) {
  const rect = canvas.value.getBoundingClientRect()
  hoverX.value = e.clientX - rect.left
  draw()
}

/** 悬浮离开：清除十字线与读数卡 */
function onLeave() {
  hoverX.value = null
  draw()
}

onMounted(() => {
  draw()
  ro = new ResizeObserver(draw)
  ro.observe(canvas.value)
})
onBeforeUnmount(() => { if (ro) ro.disconnect() })
watch(() => props.series.length, draw)
watch(resolvedTheme, draw)
</script>

<template>
  <div class="spark-wrap">
    <canvas
      ref="canvas"
      :style="{ width: '100%', height: height + 'px' }"
      @pointermove="onMove"
      @pointerleave="onLeave"
    ></canvas>
    <div
      v-if="tip"
      class="spark-tip"
      :style="{ left: tip.left + 'px', transform: tip.flip ? 'translateX(calc(-100% - 12px))' : 'translateX(12px)' }"
    >
      <div class="tip-time">{{ tip.time }}</div>
      <div class="tip-row" style="color: var(--green)">↑ {{ tip.up }}</div>
      <div class="tip-row" style="color: var(--accent)">↓ {{ tip.down }}</div>
    </div>
  </div>
</template>

<style scoped>
.spark-wrap { position: relative; }
/* 悬浮读数卡：跟随十字线水平移动，top 与绘图区上缘齐平；不接指针事件防抖动 */
.spark-tip {
  position: absolute; top: 0; z-index: 2; pointer-events: none;
  background: var(--bg-card); border: 1px solid var(--border); border-radius: 8px;
  padding: 5px 10px; box-shadow: var(--shadow);
  font-size: 12px; line-height: 1.55; white-space: nowrap;
}
.tip-time { color: var(--text-dim); font-size: 11px; }
.tip-row { font-weight: 600; font-variant-numeric: tabular-nums; }
</style>
