<script setup>
// 通用线条图标：与侧栏 SVG 同风格（24 viewBox、stroke currentColor），按 name 取路径组
import { computed } from 'vue'

const ICONS = {
  play: ['M8 5.5 18.5 12 8 18.5Z'],
  stop: ['M7.5 7.5h9v9h-9Z'],
  restart: ['M3 12a9 9 0 1 0 2.64-6.36', 'M3 3v6h6'],
  refresh: ['M21 12a9 9 0 1 1-2.64-6.36', 'M21 3v6h-6'],
  'file-code': [
    'M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7Z',
    'M14 3v4h4',
    'm10.5 12-1.5 1.5 1.5 1.5',
    'm13.5 12 1.5 1.5-1.5 1.5',
  ],
  swap: ['M4 7h13m0 0-3-3m3 3-3 3', 'M20 17H7m0 0 3-3m-3 3 3 3'],
  plus: ['M12 5v14', 'M5 12h14'],
  close: ['m6 6 12 12', 'M18 6 6 18'],
  check: ['m5 13 4 4L19 7'],
  trash: ['M4 7h16', 'M9 7V4h6v3', 'M6 7l1 13h10l1-13', 'M10 11v5', 'M14 11v5'],
  'x-square': ['M4 4h16v16H4Z', 'm9 9 6 6', 'm15 9-6 6'],
  search: ['M11 4a7 7 0 1 0 0 14 7 7 0 0 0 0-14', 'M20.5 20.5 16 16'],
  download: ['M12 4v11m0 0 4-4m-4 4-4-4', 'M5 20h14'],
  save: ['M5 4h11l3 3v13H5Z', 'M8 4v5h7V4', 'M8 20v-6h8v6'],
  upload: ['M12 20V8m0 0-4 4m4-4 4 4', 'M5 4h14'],
  zap: ['m13 2-9 12h6l-1 8 9-12h-6z'],
  'file-text': [
    'M14 3H7a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h10a1 1 0 0 0 1-1V7Z',
    'M14 3v4h4',
    'M9 13h6',
    'M9 17h6',
  ],
  cpu: [
    'M5 5h14v14H5Z',
    'M9 9h6v6H9Z',
    'M9 2v3', 'M15 2v3', 'M9 19v3', 'M15 19v3',
    'M2 9h3', 'M2 15h3', 'M19 9h3', 'M19 15h3',
  ],
  clock: ['M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20', 'M12 6v6l4 2'],
  plug: ['M12 22v-5', 'M9 8V2', 'M15 8V2', 'M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z'],
  globe: [
    'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20',
    'M2 12h20',
    'M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10Z',
  ],
  layers: ['M12 2 3 6.9l9 4.9 9-4.9Z', 'm3 11.9 9 4.9 9-4.9', 'm3 16.9 9 4.9 9-4.9'],
}

const props = defineProps({
  name: { type: String, required: true },
  size: { type: [Number, String], default: 14 },
  strokeWidth: { type: [Number, String], default: 2 },
})

const paths = computed(() => ICONS[props.name] || [])
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    :width="size"
    :height="size"
    fill="none"
    stroke="currentColor"
    :stroke-width="strokeWidth"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    style="flex: none"
  >
    <path v-for="(d, i) in paths" :key="i" :d="d" />
  </svg>
</template>
