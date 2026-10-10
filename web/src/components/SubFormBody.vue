<script setup>
// 订阅表单主体（从 SubFormModal 抽出，组合复用）：只含表单字段与提交逻辑，不带弹窗外壳。
// 复用方：
//   SubFormModal —— 订阅页/首页的添加、编辑弹窗（本组件包在 n-modal 里）；
//   SetupWizard  —— 初始化引导第二步的内嵌表单（embedded 模式，高度自适应）。
// 添加模式两种来源（单选切换）：
//   订阅链接 —— 一次添加一个（备注名只有一个，多行批量与之不匹配）；
//   手动节点 —— 多行粘贴节点分享链接（ss/ssr/vmess/vless/trojan/hysteria2/tuic/anytls，
//               支持整段 base64 订阅内容），后端转换后注入所选模板生成订阅；
// 编辑模式（传入 edit prop）单条回填：URL 订阅走原表单，手动节点订阅展示节点表单。
//
// 使用示例：
//   <SubFormBody :active="open" @added="onAdded" @close="open = false" />
//   引导内嵌：<SubFormBody embedded :active="open && step === 2" @added="onAdded" />
import { computed, ref, watch } from 'vue'
import { NButton, NInput, NRadioButton, NRadioGroup, NSelect } from 'naive-ui'
import { api } from '../api.js'
import AppIcon from './AppIcon.vue'
import { toast } from '../store.js'

const props = defineProps({
  active: Boolean, // 宿主弹窗/面板是否处于打开态：打开瞬间重置表单或按 edit 回填
  edit: { type: Object, default: null }, // 非 null 时为编辑模式
  embedded: Boolean, // 内嵌模式：不固定表单高度（外层容器自己管布局）
  showCancel: { type: Boolean, default: true }, // 是否显示取消按钮
})
const emit = defineEmits(['close', 'added', 'saved'])

const isEdit = computed(() => !!props.edit)
const isNodesEdit = computed(() => isEdit.value && props.edit.source === 'nodes')

const name = ref('')
const url = ref('')
const adding = ref(false)

// 手动节点方式
const mode = ref('url') // 添加模式来源：url=订阅链接 / nodes=手动节点
const template = ref('')
const templates = ref([])
const nodes = ref('')
const NODES_PLACEHOLDER = [
  '每行一个节点链接，支持 ss / ssr / vmess / vless / trojan / hysteria2 / tuic / anytls',
  '也可直接粘贴整段 base64 编码的订阅内容',
  'ss://YWVzLTI1Ni1nY206cGFzc0AxLjIuMy40OjQ0Mw==#香港01',
  'vmess://eyJ2IjoiMiIsInBzIjoi…',
].join('\n')

async function loadTemplates() {
  try {
    const r = await api.get('/api/templates')
    templates.value = r.templates || []
  } catch {
    templates.value = [{ name: '白名单', builtin: true }]
  }
  if (!templates.value.some(t => t.name === template.value)) {
    const def = templates.value.find(t => t.builtin) || templates.value[0]
    template.value = def ? def.name : ''
  }
}
const templateOptions = computed(() =>
  templates.value.map(t => ({ label: t.builtin ? `${t.name}（内置）` : t.name, value: t.name })))

// 内置 clash 相关 UA；很多机场按 UA 返回对应格式的配置
const CUSTOM_UA = '__custom__'
const UA_OPTIONS = [
  { value: '', label: '默认（clash-verge/clashv）' },
  { value: 'clash.meta', label: 'clash.meta（mihomo）' },
  { value: 'ClashforWindows/0.20.39', label: 'Clash for Windows' },
  { value: 'ClashMetaForAndroid/2.11.5', label: 'ClashMeta for Android' },
  { value: 'Stash/2.7.3', label: 'Stash' },
  { value: CUSTOM_UA, label: '自定义…' },
]
const uaPick = ref('')
const customUa = ref('')

// 把 UA 值映射到下拉选项：内置项直接选中，其他进「自定义」
function pickUA(ua) {
  if (ua && !UA_OPTIONS.some(o => o.value === ua)) {
    uaPick.value = CUSTOM_UA
    customUa.value = ua
  } else {
    uaPick.value = ua || ''
    customUa.value = ''
  }
}

// 打开瞬间：编辑模式回填原值；添加模式重置为初始态并预取模板/UA。
// immediate 必须开：NModal 默认 display-directive="if"，宿主弹窗打开时本组件才挂载，
// 挂载那一刻 active 已经是 true、之后再无变化，不开 immediate 初始化永远不会执行。
watch(() => props.active, (v) => {
  if (!v) return
  adding.value = false
  if (props.edit) {
    name.value = props.edit.name
    if (props.edit.source === 'nodes') {
      nodes.value = props.edit.nodes || ''
      template.value = props.edit.template || ''
      loadTemplates()
    } else {
      url.value = props.edit.url
      pickUA(props.edit.ua || '')
    }
    return
  }
  name.value = ''
  url.value = ''
  mode.value = 'url'
  nodes.value = ''
  template.value = ''
  loadTemplates()
  // 上次用的 UA 不是内置项 → 自动选中"自定义"并预填
  api.get('/api/settings').then(s => pickUA(s.custom_ua || '')).catch(() => { /* 拿不到就保持默认 */ })
}, { immediate: true })

const urlPlaceholder = computed(() =>
  isEdit.value
    ? '订阅链接'
    : '订阅链接（单次只支持一条）\nhttps://example.com/subscription')

// 部分节点行解析失败时给出 warning 提示（不影响其余节点已成功添加）
function warnSkipped(skipped) {
  if (!skipped || !skipped.length) return
  toast(`${skipped.length} 行未能识别已跳过：${skipped[0]}${skipped.length > 1 ? ' 等' : ''}`, 'warning', 6000)
}

// 添加模式：订阅链接一次一个（备注名只有一个）；手动节点整段交给后端转换注入模板
async function add() {
  if (mode.value === 'nodes') return addNodes()
  const link = url.value.trim()
  if (!link) { toast('请输入订阅链接', 'info'); return }
  if (link.split('\n').filter(s => s.trim()).length > 1) {
    toast('订阅链接一次只能添加一个，多个链接请分次添加', 'info')
    return
  }
  const ua = uaPick.value === CUSTOM_UA ? customUa.value.trim() : uaPick.value
  if (uaPick.value === CUSTOM_UA && !ua) { toast('请输入自定义 User-Agent', 'info'); return }

  adding.value = true
  try {
    const p = await api.post('/api/profiles', { name: name.value.trim(), url: link, ua })
    toast('订阅已添加', 'success')
    emit('added', p)
    emit('close')
  } catch (e) {
    toast(e.message, 'error', 6000)
  } finally {
    adding.value = false
  }
}

// 手动节点添加：单个订阅；后端把节点转换后注入模板
async function addNodes() {
  if (!nodes.value.trim()) { toast('请粘贴节点链接', 'info'); return }
  adding.value = true
  try {
    const p = await api.post('/api/profiles', {
      name: name.value.trim(),
      template: template.value,
      nodes: nodes.value,
    })
    toast(`已添加订阅（${p.skipped?.length ? `${p.skipped.length} 行无法识别已跳过` : '节点已全部转换'}）`, 'success')
    warnSkipped(p.skipped)
    emit('added', p)
    emit('close')
  } catch (e) {
    toast(e.message, 'error', 6000)
  } finally {
    adding.value = false
  }
}

// 编辑模式：URL 订阅保存名称/地址/UA；手动节点订阅保存名称/模板/节点。
// 地址（或节点）变化时后端自动重建内容
async function saveEdit() {
  adding.value = true
  try {
    if (isNodesEdit.value) {
      const r = await api.put(`/api/profiles/${props.edit.id}`, {
        name: name.value.trim(),
        template: template.value,
        nodes: nodes.value,
      })
      toast('订阅已保存' + (r.redownloaded ? '，已重新生成配置' + (r.restarted ? '并重载内核' : '') : ''), 'success')
      warnSkipped(r.skipped)
      emit('saved', r.profile)
      emit('close')
      return
    }
    const r = await api.put(`/api/profiles/${props.edit.id}`, {
      name: name.value.trim(),
      url: url.value.trim(),
      ua: uaPick.value === CUSTOM_UA ? customUa.value.trim() : uaPick.value,
    })
    toast('订阅已保存' + (r.redownloaded ? '，已按新配置重新下载' + (r.restarted ? '并重载内核' : '') : ''), 'success')
    emit('saved', r.profile)
    emit('close')
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    adding.value = false
  }
}

function submit() {
  if (isEdit.value) saveEdit()
  else add()
}
</script>

<template>
  <!-- embedded：无固定高度（引导等外层容器自己管布局）；弹窗模式恒高防切换跳动 -->
  <div class="form" :class="{ 'form-auto': isEdit || embedded }">
    <div class="form-body">
      <!-- 添加模式：选择来源；手动节点订阅的编辑也走节点表单（无来源切换） -->
      <n-radio-group v-if="!isEdit" v-model:value="mode" size="small">
        <n-radio-button value="url">订阅链接</n-radio-button>
        <n-radio-button value="nodes">手动节点</n-radio-button>
      </n-radio-group>

      <n-input v-model:value="name" :placeholder="isEdit ? '备注名（留空保持原名）' : '备注名（可选）'" @keyup.enter="submit" />

      <!-- 手动节点表单：模板 + 节点文本（添加与节点类编辑共用） -->
      <template v-if="mode === 'nodes' || isNodesEdit">
        <n-select v-model:value="template" :options="templateOptions" placeholder="选择配置模板" />
        <n-input
          v-model:value="nodes"
          type="textarea"
          :rows="8"
          class="nodes-area"
          :placeholder="isNodesEdit ? '节点链接，留空保持原节点（仅改名/换模板）\n' + NODES_PLACEHOLDER : NODES_PLACEHOLDER"
        />
        <p class="page-sub">节点将转换后注入模板的 proxies 段，并替换 proxy-groups 里的 __ALL_PROXIES__ 占位符</p>
      </template>

      <!-- 订阅链接表单：一次一个；高度给自定义 UA 输入框留出余量（6 行不出现内部滚动条） -->
      <template v-else>
        <n-input
          v-model:value="url"
          type="textarea"
          :rows="isEdit ? 2 : 6"
          :placeholder="urlPlaceholder"
        />
        <n-select v-model:value="uaPick" :options="UA_OPTIONS" />
        <n-input
          v-if="uaPick === CUSTOM_UA"
          v-model:value="customUa"
          placeholder="自定义 User-Agent，如 clash.meta/1.19.31"
          @keyup.enter="submit"
        />
        <p class="page-sub">部分机场按 UA 返回不同格式的配置，更新订阅时沿用添加时的 UA</p>
      </template>
    </div>

    <div class="actions">
      <n-button v-if="showCancel" quaternary @click="emit('close')"><template #icon><AppIcon name="close" :size="13" /></template>取消</n-button>
      <n-button v-if="isEdit" type="primary" :loading="adding" @click="submit">
        <template #icon><AppIcon name="check" :size="14" /></template>保存
      </n-button>
      <n-button v-else type="primary" :loading="adding" @click="submit">
        <template #icon><AppIcon name="plus" :size="14" /></template>添加
      </n-button>
    </div>
  </div>
</template>

<style scoped>
/* 弹窗内高度恒定：主体固定高度，按钮钉在底部，链接/手动节点两种模式切换时
   弹窗不再一会儿高一会儿低；编辑/内嵌模式布局不切换，用自适应高度免大段留白 */
.form { display: flex; flex-direction: column; height: 420px; max-height: calc(100vh - 160px); }
.form-auto { height: auto; }
.form-body { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 12px; }
.actions { flex: none; display: flex; justify-content: flex-end; gap: 8px; padding-top: 12px; }
/* 节点/模板文本用等宽字体，链接更易读 */
.nodes-area :deep(textarea) { font-family: var(--mono, ui-monospace, monospace); font-size: 12px; }
</style>
