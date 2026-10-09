<script setup>
// 设置项名称旁的问号图标：点击弹出说明弹窗（功能作用 / 开关影响 / 默认值建议），
// 说明不占设置页行内空间
import { ref } from 'vue'
import { NModal } from 'naive-ui'
import AppIcon from './AppIcon.vue'

defineProps({
  title: { type: String, required: true },
  rows: { type: Array, required: true }, // [{ k: 小节标题, v: 说明文字 }]
})

const show = ref(false)
</script>

<template>
  <span class="help-wrap" title="查看说明" @click="show = true">
    <AppIcon name="help" :size="13" />
  </span>
  <n-modal
    preset="card" :title="title" :show="show"
    :style="{ width: '580px', maxWidth: '94vw' }"
    @update:show="show = false"
  >
    <div class="hm-rows">
      <div v-for="(r, i) in rows" :key="i" class="hm-row">
        <span class="hm-k">{{ r.k }}</span>
        <span class="hm-v">{{ r.v }}</span>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
/* 行内 SVG 图标与文字同行必须包 flex 居中，否则与名称基线错位 */
.help-wrap {
  display: inline-flex;
  align-items: center;
  color: var(--text-dim);
  cursor: pointer;
}
.help-wrap:hover { color: var(--accent); }
.hm-rows { display: flex; flex-direction: column; gap: 14px; }
.hm-row { display: flex; flex-direction: column; gap: 4px; }
.hm-k { color: var(--accent); font-size: 12px; font-weight: 600; }
.hm-v { color: var(--text-dim); font-size: 13px; line-height: 1.7; }
</style>
