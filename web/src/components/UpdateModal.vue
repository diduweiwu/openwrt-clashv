<script setup>
/**
 * 升级确认弹窗：新版本号徽标（主题色醒目展示）+ Release 更新日志（超长竖向
 * 滚动）+ 升级/取消按钮。供首页与设置页的插件/内核升级确认共用，替代原先
 * 纯文本的 ask() 弹窗。
 *
 * 日志为 Markdown 源文本，这里做轻量渲染（不引入 markdown 依赖、不拼 HTML，
 * 全部走插值防 XSS）：# 标题加粗、- / 数字. 列表加圆点或保留序号、其余按段落，
 * 行内 **加粗**、`代码` 等记号直接剥掉符号只留纯文本。
 *
 * 用法示例（配合检查更新接口的返回值）：
 *   <UpdateModal v-model:show="updateShow" title="插件更新"
 *                :latest="updateInfo.latest" :current="'v' + updateInfo.current"
 *                :notes="updateInfo.notes" :pkg-text="pkgText"
 *                @confirm="updateShow = false; upgradePlugin()" />
 */
import { computed } from 'vue'
import { NButton, NModal } from 'naive-ui'

const props = defineProps({
  show: Boolean,    // 弹窗显隐（v-model:show）
  title: String,    // 弹窗标题，如「插件更新」「升级内核」
  latest: String,   // 新版本号（徽标醒目展示，直接传 Release tag，如 v1.2.4）
  current: String,  // 当前版本号（可空，空时不显示）
  notes: String,    // Release 更新日志原文（Markdown），可空
  pkgText: String,  // 匹配包说明行（可空，空时不显示）
  confirmText: { type: String, default: '立即升级' }, // 确认按钮文案
})
const emit = defineEmits(['update:show', 'confirm'])

// 是否有可展示的日志（全空白视为无）
const hasNotes = computed(() => !!(props.notes && props.notes.trim()))

// 日志轻量解析：标题/列表项/段落三种块，剥掉行内 Markdown 记号只留纯文本
const blocks = computed(() => {
  if (!hasNotes.value) return []
  return props.notes.split(/\r?\n/).map((line) => {
    const t = line.trim()
    if (!t) return { type: 'space' }
    const clean = (s) => s.replace(/[*_`]{1,3}/g, '')
    let m = t.match(/^#{1,6}\s+(.*)/)
    if (m) return { type: 'h', text: clean(m[1]) }
    m = t.match(/^[-*+]\s+(.*)/)
    if (m) return { type: 'li', text: clean(m[1]), num: '' }
    // 有序列表保留原序号（更新日志里常用来分条，序号即条目本身）
    m = t.match(/^(\d+[.)])\s+(.*)/)
    if (m) return { type: 'li', text: clean(m[2]), num: m[1] }
    return { type: 'p', text: clean(t) }
  })
})

function close() {
  emit('update:show', false)
}
</script>

<template>
  <n-modal
    preset="card"
    :title="title"
    :show="show"
    :style="{ width: '540px', maxWidth: '96vw' }"
    @update:show="(v) => emit('update:show', v)"
  >
    <div class="up-hero">
      <span class="up-new mono">{{ latest }}</span>
      <span v-if="current" class="up-cur">当前 {{ current }}</span>
    </div>

    <p v-if="pkgText" class="up-pkg">{{ pkgText }}</p>

    <div class="up-notes-label">更新日志</div>
    <!-- 竖向滚动：日志超长时只在框内滚动，弹窗高度保持稳定 -->
    <div v-if="hasNotes" class="up-notes">
      <template v-for="(b, i) in blocks" :key="i">
        <div v-if="b.type === 'h'" class="up-h">{{ b.text }}</div>
        <div v-else-if="b.type === 'li'" class="up-li">
          <span class="up-dot">{{ b.num || '•' }}</span><span>{{ b.text }}</span>
        </div>
        <p v-else-if="b.type === 'p'" class="up-p">{{ b.text }}</p>
      </template>
    </div>
    <div v-else class="up-notes up-none">该版本没有填写更新日志</div>

    <div class="up-foot">
      <n-button @click="close">取消</n-button>
      <n-button type="primary" @click="emit('confirm'); close()">{{ confirmText }}</n-button>
    </div>
  </n-modal>
</template>

<style scoped>
/* 新版本号徽标：主题色描边 + 淡彩底 + 加粗放大，一眼锁定 */
.up-hero { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.up-new {
  font-size: 19px; font-weight: 700; color: var(--accent);
  background: var(--accent-soft); border: 1.5px solid rgba(91, 107, 240, 0.45);
  padding: 4px 16px; border-radius: 999px;
}
.up-cur { color: var(--text-dim); font-size: 13px; }

.up-pkg { color: var(--text-dim); font-size: 12.5px; margin-bottom: 10px; }

.up-notes-label { font-size: 13px; font-weight: 600; margin: 2px 0 8px; }
.up-notes {
  max-height: 320px; overflow-y: auto;
  background: var(--bg-card-2); border: 1px solid var(--border); border-radius: 10px;
  padding: 12px 14px; font-size: 13px; line-height: 1.75;
}
.up-h { font-weight: 600; margin: 8px 0 2px; }
.up-h:first-child { margin-top: 0; }
.up-li { display: flex; gap: 7px; padding-left: 2px; }
.up-dot { color: var(--accent); flex: none; min-width: 14px; }
.up-p { margin: 2px 0; }
.up-none { color: var(--text-dim); }

.up-foot { display: flex; justify-content: flex-end; gap: 10px; margin-top: 16px; }
</style>
