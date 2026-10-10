<script setup>
// 添加/编辑订阅弹窗（订阅页与首页共用）：由 open prop 控制显隐。
// 表单字段与提交逻辑在 SubFormBody（本组件只提供弹窗外壳），两者组合复用——
// 初始化引导（SetupWizard）内嵌同一份表单主体，改字段行为只需改一处。
import { NModal } from 'naive-ui'
import SubFormBody from './SubFormBody.vue'

const props = defineProps({
  open: Boolean,
  edit: { type: Object, default: null }, // 非 null 时为编辑模式
})
const emit = defineEmits(['close', 'added', 'saved'])
</script>

<template>
  <n-modal
    preset="card"
    :title="edit ? '编辑订阅' : '添加订阅'"
    :show="open"
    :style="{ width: '560px', maxWidth: '94vw' }"
    @update:show="emit('close')"
  >
    <SubFormBody
      :active="open"
      :edit="edit"
      @added="emit('added', $event)"
      @saved="emit('saved', $event)"
      @close="emit('close')"
    />
  </n-modal>
</template>
