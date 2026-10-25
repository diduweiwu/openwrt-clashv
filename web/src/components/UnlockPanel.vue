<script setup>
/**
 * 首页「解锁测试」面板：内置预设站点 + 用户自定义站点，经当前代理出口逐个测试
 * 可达性（POST /api/unlock/test，后端经本机混合端口发起 GET）。
 *
 * 判定：HTTP 2xx/3xx → 支持；403/404/451 → 不支持（区域封锁的典型状态码，
 * 如 Netflix 对无版权影片返回 404）；其余状态/传输层失败 → 测试失败。
 * 结果缓存在 localStorage（unlockSites.js），刷新页面仍能看到最近一次结果。
 *
 * 用法示例：
 *   <UnlockPanel />
 */
import { computed, ref } from 'vue'
import { NButton, NCard, NEmpty, NInput, NModal, NTag } from 'naive-ui'
import { api } from '../api.js'
import { ask, toast, fmtTime } from '../store.js'
import AppIcon from './AppIcon.vue'
import { PRESET_SITES, customSites, saveResults, resultOf } from '../unlockSites.js'

// 面板站点列表 = 预设 + 自定义（顺序：预设在前，自定义追加在后）
const sites = computed(() => [...PRESET_SITES, ...customSites.value])

// 正在测试的站点集合（"name|url"），全部测试时为 '__all__'
const testing = ref(new Set())
const testingAll = ref(false)

// 单个站点的结果与展示状态
function view(site) {
  const r = resultOf(site)
  if (!r) return { tag: '未测试', type: 'default', detail: '', at: 0 }
  const at = r.at || 0
  if (r.ok) return { tag: '支持', type: 'success', detail: detailLine(r), at }
  if ([403, 404, 451].includes(r.code)) return { tag: '不支持', type: 'error', detail: `HTTP ${r.code}`, at }
  return { tag: '测试失败', type: 'warning', detail: r.err || '未知错误', at }
}

function detailLine(r) {
  const parts = []
  if (r.code) parts.push(`HTTP ${r.code}`)
  if (r.ms) parts.push(`${r.ms}ms`)
  return parts.join(' · ')
}

function isTesting(site) {
  return testing.value.has(site.name + '|' + site.url)
}

// 测试：传入站点子集；all=true 时测全部并显示整体 loading
async function runTest(list, all = false) {
  if (!list.length) return
  if (all) testingAll.value = true
  const keys = list.map(s => s.name + '|' + s.url)
  testing.value = new Set([...testing.value, ...keys])
  try {
    const r = await api.post('/api/unlock/test', { sites: list })
    saveResults(r.results || [])
  } catch (e) {
    toast(e.message, 'error')
  } finally {
    const rest = new Set(testing.value)
    keys.forEach(k => rest.delete(k))
    testing.value = rest
    if (all) testingAll.value = false
  }
}

function testAll() {
  runTest(sites.value, true)
}

function testOne(site) {
  runTest([site])
}
// ---- 添加自定义站点 ----
const showAdd = ref(false)
const addName = ref('')
const addURL = ref('')
const addBusy = ref(false)

function openAdd() {
  addName.value = ''
  addURL.value = ''
  showAdd.value = true
}

// 保存前规整：URL 缺协议补 https://；重名/重址不重复添加
async function saveSite() {
  const url = addURL.value.trim()
  if (!url) { toast('请填写测试地址', 'error'); return }
  const name = addName.value.trim() || url.replace(/^https?:\/\//, '').split('/')[0]
  const norm = /^(https?:\/\/)/i.test(url) ? url : 'https://' + url
  const dup = sites.value.some(s => s.url === norm)
  if (dup) { toast('该地址已在测试列表中', 'error'); return }
  addBusy.value = true
  const site = { name, url: norm }
  customSites.value = [...customSites.value, site]
  showAdd.value = false
  addBusy.value = false
  runTest([site]) // 加完顺手测一次
}

async function removeSite(site) {
  if (!(await ask('删除站点', `不再测试「${site.name}」？`))) return
  customSites.value = customSites.value.filter(s => s.url !== site.url)
}

const isCustom = (site) => customSites.value.some(s => s.url === site.url)
const hint = computed(() => '经当前代理出口测试 · 2xx/3xx 视为支持')
</script>

<template>
  <n-card>
    <div class="sec-head">
      <h3><AppIcon class="sec-ico" name="globe" :size="15" />解锁测试</h3>
      <div class="unlock-actions">
        <span class="page-sub">{{ hint }}</span>
        <n-button size="small" title="添加自定义测试网站" @click="openAdd">
          <template #icon><AppIcon name="plus" :size="13" /></template>添加网站
        </n-button>
        <n-button size="small" type="primary" secondary :loading="testingAll" @click="testAll">
          <template #icon><AppIcon name="refresh" :size="13" /></template>测试全部
        </n-button>
      </div>
    </div>

    <div class="unlock-grid">
      <div v-for="site in sites" :key="site.name + '|' + site.url" class="u-item">
        <div class="u-head">
          <span class="u-name" :title="site.url">{{ site.name }}</span>
          <span class="u-ops">
            <button class="u-btn" :title="`重测 ${site.name}`" :disabled="isTesting(site)" @click="testOne(site)">
              <AppIcon name="refresh" :size="12" :class="{ spin: isTesting(site) }" />
            </button>
            <button v-if="isCustom(site)" class="u-btn danger" title="删除该站点" @click="removeSite(site)">
              <AppIcon name="trash" :size="12" />
            </button>
          </span>
        </div>
        <div class="u-status">
          <n-tag size="small" round :bordered="false" :type="isTesting(site) ? 'info' : view(site).type">
            {{ isTesting(site) ? '测试中…' : view(site).tag }}
          </n-tag>
          <!-- 状态码+耗时贴标签行右侧：底部一行放不下会被截断（HT...），完整内容悬浮可见 -->
          <span class="u-code mono" :title="view(site).detail">{{ isTesting(site) ? '' : view(site).detail }}</span>
        </div>
        <div class="u-foot">
          <span class="u-time">{{ view(site).at ? fmtTime(view(site).at) : '' }}</span>
        </div>
      </div>
    </div>
    <n-empty
      v-if="sites.length === 0"
      description="还没有测试站点，点右上角「添加网站」"
      style="padding: 30px 0"
    />
  </n-card>

  <!-- 添加自定义站点弹窗 -->
  <n-modal
    preset="card"
    title="添加测试网站"
    :show="showAdd"
    :style="{ width: '460px', maxWidth: '94vw' }"
    @update:show="showAdd = false"
  >
    <div class="add-body">
      <n-input v-model:value="addName" placeholder="名称（选填，默认取域名）" clearable />
      <n-input v-model:value="addURL" placeholder="测试地址，如 https://example.com" clearable @keyup.enter="saveSite" />
      <span class="page-sub">测试方式：经当前代理出口 GET 该地址，能正常打开（HTTP 2xx/3xx）即判定支持</span>
      <div class="add-foot">
        <n-button @click="showAdd = false">取消</n-button>
        <n-button type="primary" :loading="addBusy" @click="saveSite">
          <template #icon><AppIcon name="check" :size="13" /></template>添加
        </n-button>
      </div>
    </div>
  </n-modal>
</template>

<style scoped>
/* 卡片标题行与图标：与首页各卡片的 sec-head 同款（scoped 隔离，需在本组件重复声明） */
.sec-head { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; margin-bottom: 14px; flex-wrap: wrap; border-bottom: 1px dashed var(--border); padding-bottom: 12px; }
.sec-head h3 { font-size: 15px; }
.sec-ico { color: var(--accent); margin-right: 7px; vertical-align: -2px; }
.unlock-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
/* 站点瓦片网格：自适应列数，与切换节点卡片网格同规格 */
.unlock-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(168px, 1fr));
  gap: 9px;
}
.u-item {
  display: flex; flex-direction: column; gap: 6px;
  background: var(--bg-card-2);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px 12px;
  min-width: 0;
}
.u-head { display: flex; align-items: center; gap: 6px; }
.u-name {
  flex: 1; min-width: 0;
  font-size: 13px; font-weight: 600;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.u-ops { display: inline-flex; gap: 2px; flex: none; }
.u-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: 20px; height: 20px; padding: 0;
  border: none; border-radius: 5px;
  background: transparent; cursor: pointer;
  color: var(--text-dim); opacity: 0.6;
}
.u-btn:hover { opacity: 1; color: var(--accent); background: var(--hover); }
.u-btn.danger:hover { color: var(--red); }
.u-btn:disabled { cursor: default; opacity: 0.6; color: var(--text-dim); }
.u-status {
  display: flex; align-items: center; justify-content: space-between; gap: 8px;
}
.u-code {
  flex: 1; min-width: 0; text-align: right;
  color: var(--text-dim); font-size: 11px;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.u-foot {
  display: flex; align-items: center; justify-content: flex-end; gap: 8px;
  border-top: 1px dashed var(--border); padding-top: 6px;
}
.u-time { color: var(--text-dim); font-size: 11px; flex: none; }
.spin { animation: u-spin 0.9s linear infinite; }
@keyframes u-spin { to { transform: rotate(360deg); } }
.add-body { display: flex; flex-direction: column; gap: 10px; }
.add-foot { display: flex; justify-content: flex-end; gap: 10px; margin-top: 4px; }
</style>
