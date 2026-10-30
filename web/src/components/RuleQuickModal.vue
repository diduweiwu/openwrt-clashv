<script setup>
// 快速添加自定义规则弹窗：从连接页右键菜单「自定义…」进入，类型/值已按连接目标预填。
// 保存复用 appendCustomRule（拉现有列表 → 查重 → 追加 → 全量覆写，内核运行时自动重载），
// 与规则页弹窗同一表单形态：类型 / 值 / 目标 / no-resolve / 实时预览。
import { computed, ref, watch } from 'vue'
import { NButton, NCheckbox, NInput, NModal, NSelect } from 'naive-ui'
import { toast } from '../store.js'
import { RULE_TYPES, NORESOLVE_TYPES, VALUE_HINT, appendCustomRule } from '../ruleTypes.js'
import AppIcon from './AppIcon.vue'

const props = defineProps({
  // 弹窗显隐（v-model:show）
  show: { type: Boolean, default: false },
  // 打开时的预填初值：{ type, value, noResolve }，来自连接目标推荐
  init: { type: Object, default: () => ({ type: 'DOMAIN-SUFFIX', value: '', noResolve: false }) },
  // 目标候选（代理组名，不含 DIRECT/REJECT）：由父组件随内核运行态刷新
  targets: { type: Array, default: () => [] },
  // 弹窗挂载位置：默认 body；全屏态下传入表格卡元素，避免浮层被系统全屏吞掉
  to: { type: [String, Object], default: 'body' },
})
const emit = defineEmits(['update:show'])

const form = ref({ type: 'DOMAIN-SUFFIX', value: '', target: 'DIRECT', noResolve: false })
const saving = ref(false)

// 弹窗每次打开按预填初值重置（目标回退 DIRECT 由用户重选）
watch(() => props.show, (v) => {
  if (v) form.value = { target: 'DIRECT', ...props.init }
})

const noResolveVisible = computed(() => NORESOLVE_TYPES.includes(form.value.type))
const valuePlaceholder = computed(() =>
  form.value.type === 'MATCH' ? '兜底规则无需填写值' : `例：${VALUE_HINT[form.value.type] || ''}`,
)

// 目标候选：代理组在前，DIRECT/REJECT 兜底在后（与规则页下拉同一形态）
const targetOptions = computed(() => {
  const opts = props.targets.map(n => ({ label: n, value: n }))
  for (const t of ['DIRECT', 'REJECT']) {
    if (!props.targets.includes(t)) opts.push({ label: t, value: t })
  }
  return opts
})

// 三段合成；MATCH 只有目标一段，IP 类规则按勾选拼 no-resolve
const composedRule = computed(() => {
  const f = form.value
  if (f.type === 'MATCH') return `MATCH,${f.target}`
  const v = f.value.trim()
  if (!v) return ''
  return `${f.type},${v},${f.target}${f.noResolve ? ',no-resolve' : ''}`
})

/**
 * 保存规则：合成规则串追加进自定义规则列表，成功后关闭弹窗。
 * 查重命中 / 保存失败时保留弹窗供用户修改（提示由 appendCustomRule 统一给出）。
 *
 * @returns {Promise<void>}
 */
async function saveRule() {
  if (!composedRule.value || saving.value) return
  saving.value = true
  try {
    if (await appendCustomRule(composedRule.value)) emit('update:show', false)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <n-modal
    preset="card"
    title="添加规则"
    :show="show"
    :style="{ width: '580px', maxWidth: '94vw' }"
    :to="to"
    @update:show="v => emit('update:show', v)"
  >
    <div class="rule-form">
      <div class="f-row">
        <span class="f-label">类型</span>
        <n-select v-model:value="form.type" :options="RULE_TYPES" :consistent-menu-width="false" />
      </div>
      <div class="f-row" v-if="form.type !== 'MATCH'">
        <span class="f-label">值</span>
        <n-input v-model:value="form.value" :placeholder="valuePlaceholder" />
      </div>
      <div class="f-row">
        <span class="f-label">目标</span>
        <n-select v-model:value="form.target" :options="targetOptions" placeholder="目标" />
      </div>
      <div class="f-row" v-if="noResolveVisible">
        <span class="f-label">选项</span>
        <n-checkbox v-model:checked="form.noResolve">no-resolve（命中后不做 DNS 解析，路由器上推荐）</n-checkbox>
      </div>
      <div class="f-preview mono">预览：{{ composedRule || '—' }}</div>
      <div class="f-foot">
        <span class="page-sub">保存后置顶生效，内核运行中自动重载</span>
        <div class="btn-pair">
          <n-button size="small" :disabled="saving" @click="emit('update:show', false)">取消</n-button>
          <n-button type="primary" size="small" :loading="saving" :disabled="form.type !== 'MATCH' && !form.value.trim()" @click="saveRule">
            <template #icon><AppIcon name="check" :size="13" /></template>保存
          </n-button>
        </div>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
/* 弹窗表单：标签在上、控件在下，底部实时预览合成结果（与规则页弹窗同款） */
.rule-form { display: flex; flex-direction: column; gap: 14px; }
.f-row { display: flex; flex-direction: column; gap: 6px; }
.f-label { font-size: 12.5px; color: var(--text-dim); }
.f-preview {
  padding: 9px 12px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12.5px;
  color: var(--accent);
  word-break: break-all;
}
.f-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.btn-pair { display: flex; gap: 8px; }
</style>
