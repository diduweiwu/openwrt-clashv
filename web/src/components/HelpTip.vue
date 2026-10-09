<script setup>
// 设置项名称旁的问号图标：悬浮展示默认值/建议说明，不占行内空间
import { NTooltip } from 'naive-ui'
import AppIcon from './AppIcon.vue'

defineProps({ text: { type: String, required: true } })
</script>

<template>
  <n-tooltip trigger="hover" placement="top" :content-style="{ maxWidth: '340px', lineHeight: '1.6' }">
    <template #trigger>
      <span class="help-wrap"><AppIcon name="help" :size="13" /></span>
    </template>
    {{ text }}
  </n-tooltip>
</template>

<style scoped>
/* 行内 SVG 图标与文字同行必须包 flex 居中，否则与名称基线错位 */
.help-wrap {
  display: inline-flex;
  align-items: center;
  color: var(--text-dim);
  cursor: help;
}
.help-wrap:hover { color: var(--accent); }
</style>
