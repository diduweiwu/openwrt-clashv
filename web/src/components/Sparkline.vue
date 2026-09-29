<script setup>
// 手绘 canvas 迷你流量曲线（无第三方图表库，保持小体积低内存）
import { onMounted, ref, watch } from 'vue'

const props = defineProps({
  series: { type: Array, default: () => [] }, // [{up, down}]
  height: { type: Number, default: 120 },
})

const canvas = ref(null)

function draw() {
  const el = canvas.value
  if (!el) return
  const dpr = window.devicePixelRatio || 1
  const w = el.clientWidth
  const h = props.height
  el.width = w * dpr
  el.height = h * dpr
  const ctx = el.getContext('2d')
  ctx.scale(dpr, dpr)
  ctx.clearRect(0, 0, w, h)

  const data = props.series.slice(-120)
  const n = data.length
  if (n < 2) return

  let max = 1024 * 8
  for (const p of data) max = Math.max(max, p.up, p.down)
  const x = (i) => (i / (n - 1)) * (w - 2) + 1
  const y = (v) => h - 6 - (v / max) * (h - 14)

  const line = (key, stroke, fill) => {
    ctx.beginPath()
    ctx.moveTo(x(0), y(data[0][key]))
    for (let i = 1; i < n; i++) ctx.lineTo(x(i), y(data[i][key]))
    ctx.strokeStyle = stroke
    ctx.lineWidth = 1.8
    ctx.lineJoin = 'round'
    ctx.stroke()
    ctx.lineTo(x(n - 1), h)
    ctx.lineTo(x(0), h)
    ctx.closePath()
    const g = ctx.createLinearGradient(0, 0, 0, h)
    g.addColorStop(0, fill)
    g.addColorStop(1, 'transparent')
    ctx.fillStyle = g
    ctx.fill()
  }
  line('down', '#5b6bf0', 'rgba(91, 107, 240, 0.22)')
  line('up', '#3fb96f', 'rgba(63, 185, 111, 0.18)')
}

onMounted(draw)
watch(() => props.series.length, draw)
</script>

<template>
  <canvas ref="canvas" :style="{ width: '100%', height: height + 'px' }"></canvas>
</template>
