<script setup>
// 手绘 canvas 流量曲线（无第三方图表库，保持小体积低内存）
// 带坐标轴刻度（Y 速率 / X 时间）与悬浮读数：悬浮出十字线 + 读数卡显示对应时刻的
// 上传/下载速率，贴右侧自动向左翻转，交互对齐 Clash Verge 的流量图
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
let ro = null     // ResizeObserver：容器宽度变化（侧栏收起/转屏）时重绘

/**
 * 把 Y 轴满程值向上取整到 2 的幂（8KB 起步），刻度标签是好看的整数速率。
 * 注意不能只在单一数量级内取「1/2/4/8/16 倍」：数据可能达基数的数百倍
 * （如 576 KB/s = 576×1KB），那样取出的满程反而小于数据、曲线顶出绘图区。
 *
 * @param {number} v 待取整的速率下界（字节/秒），须 > 0
 * @returns {number} 取整后的满程值，恒 >= v 且 >= 8KB
 *
 * @example
 * niceCeil(589_824) // → 1_048_576（1 MB/s）
 */
function niceCeil(v) {
  let m = 1024 * 8
  while (m < v) m *= 2
  return m
}

/**
 * 毫秒时间戳 → HH:MM:SS 文案，用于 X 轴刻度与悬浮读数卡。
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
 * 准备一帧绘制所需的全部上下文：画布尺寸/DPR、数据、坐标映射与时间取值。
 * 数据不足两点时清掉悬浮卡并返回 null，调用方据此跳过后续绘制。
 *
 * @returns {object|null} 场景对象 {ctx,w,h,data,n,max,theme,px0,px1,py0,py1,X,Y,timeAt}
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

  const data = props.series.slice(-120)
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
  const X = (i) => px0 + (i / (n - 1)) * (px1 - px0)
  const Y = (v) => py1 - (v / max) * (py1 - py0)
  const lastT = data[n - 1].t ?? Date.now()
  const timeAt = (i) => data[i].t ?? (lastT - (n - 1 - i) * 1000)
  return { ctx, w, h, data, n, max, theme, px0, px1, py0, py1, X, Y, timeAt }
}

/**
 * 绘制坐标轴：Y 轴三档速率刻度（0/半程/满程）+ 横向网格线 + 底部刻度小竖线。
 *
 * @param {object} s buildScene 返回的场景对象
 * @returns {void}
 *
 * @example
 * drawAxes(scene)
 */
function drawAxes(s) {
  const { ctx, n, max, theme, px0, px1, py0, py1, X, Y, timeAt } = s
  // Y 轴：右对齐画在留白里，网格线横贯绘图区
  ctx.textAlign = 'right'
  ctx.textBaseline = 'middle'
  for (const v of [0, max / 2, max]) {
    ctx.fillStyle = theme.dim
    ctx.fillText(fmtRate(v), px0 - 6, Y(v))
    ctx.strokeStyle = theme.border
    ctx.beginPath(); ctx.moveTo(px0, Y(v)); ctx.lineTo(px1, Y(v)); ctx.stroke()
  }
  // X 轴：按绘图区宽度自适应刻度数（5/3/2 个），再以数据点数封顶（点少时多刻度会
  // 经四舍五入落到同一索引叠印标签）；首刻度左对齐、尾刻度右对齐防出界
  const plotW = px1 - px0
  const slots = Math.min(plotW / 4 >= 76 ? 4 : plotW / 2 >= 76 ? 2 : 1, n - 1)
  ctx.textBaseline = 'top'
  for (let k = 0; k <= slots; k++) {
    const i = Math.round((k * (n - 1)) / slots)
    const x = X(i)
    ctx.fillStyle = theme.dim
    ctx.textAlign = k === 0 ? 'left' : k === slots ? 'right' : 'center'
    ctx.fillText(fmtClock(timeAt(i)), x, py1 + 5)
    ctx.strokeStyle = theme.border
    ctx.beginPath(); ctx.moveTo(x, py1); ctx.lineTo(x, py1 + 3); ctx.stroke()
  }
}

/**
 * 绘制上传/下载两条曲线及其渐变面积填充（颜色与上方速率瓦片一一对应）。
 *
 * @param {object} s 场景对象
 * @returns {void}
 *
 * @example
 * drawSeries(scene)
 */
function drawSeries(s) {
  const { ctx, data, n, h, py0, py1, X, Y } = s
  const line = (key, stroke, fill) => {
    ctx.beginPath()
    ctx.moveTo(X(0), Y(data[0][key]))
    for (let i = 1; i < n; i++) ctx.lineTo(X(i), Y(data[i][key]))
    ctx.strokeStyle = stroke
    ctx.lineWidth = 1.8
    ctx.lineJoin = 'round'
    ctx.stroke()
    ctx.lineTo(X(n - 1), py1)
    ctx.lineTo(X(0), py1)
    ctx.closePath()
    const g = ctx.createLinearGradient(0, py0, 0, h)
    g.addColorStop(0, fill)
    g.addColorStop(1, 'transparent')
    ctx.fillStyle = g
    ctx.fill()
  }
  line('down', '#5b6bf0', 'rgba(91, 107, 240, 0.22)')
  line('up', '#3fb96f', 'rgba(63, 185, 111, 0.18)')
}

/**
 * 绘制悬浮辅助元素并同步读数卡：竖虚线 + 两系列交点圆点 + tip 内容。
 * 索引由悬浮横坐标实时换算，数据每秒滚动后仍指向鼠标所指的「N 秒前」。
 *
 * @param {object} s 场景对象
 * @returns {void}
 *
 * @example
 * drawHover(scene)
 */
function drawHover(s) {
  const { ctx, data, n, theme, px0, px1, py0, py1, X, Y, timeAt } = s
  if (hoverX.value == null) { tip.value = null; return }
  const i = Math.min(n - 1, Math.max(0, Math.round(((hoverX.value - px0) / (px1 - px0)) * (n - 1))))
  const x = X(i)
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
