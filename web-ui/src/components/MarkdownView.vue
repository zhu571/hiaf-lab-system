<template>
  <!-- html:false 下 markdown-it 输出已转义原始 HTML，v-html 安全 -->
  <div class="markdown-view" v-html="rendered" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import MarkdownIt from 'markdown-it'

const props = defineProps<{ source?: string | null }>()

// C14 展示侧 Markdown 渲染：html:false 防注入；linkify 自动识别 URL；
// breaks 把换行转成 <br>，保持原 pre-wrap 纯文本的观感
const md = new MarkdownIt({ html: false, linkify: true, breaks: true })

// 链接统一新窗口打开并带 noopener（等价于内置默认 link_open 再补两个属性）
md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  tokens[idx].attrSet('target', '_blank')
  tokens[idx].attrSet('rel', 'noopener')
  return self.renderToken(tokens, idx, options)
}

const rendered = computed(() => md.render(props.source || ''))
</script>

<style scoped>
.markdown-view {
  color: var(--text-2);
  font-size: 13px;
  line-height: 1.7;
  overflow-wrap: break-word;
}

.markdown-view :deep(h1),
.markdown-view :deep(h2),
.markdown-view :deep(h3),
.markdown-view :deep(h4) {
  color: var(--text-1);
  line-height: 1.4;
  margin: 14px 0 8px;
}

.markdown-view :deep(h1) {
  font-size: 18px;
}

.markdown-view :deep(h2) {
  font-size: 16px;
}

.markdown-view :deep(h3) {
  font-size: 14px;
}

.markdown-view :deep(h4) {
  font-size: 13px;
}

.markdown-view :deep(p) {
  margin: 8px 0;
}

.markdown-view :deep(ul),
.markdown-view :deep(ol) {
  margin: 8px 0;
  padding-left: 22px;
}

.markdown-view :deep(li) {
  margin: 2px 0;
}

.markdown-view :deep(code) {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-size: 12px;
  padding: 1px 5px;
}

.markdown-view :deep(pre) {
  background: var(--surface-2);
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: auto;
  padding: 10px;
}

.markdown-view :deep(pre code) {
  background: none;
  border: none;
  padding: 0;
}

.markdown-view :deep(blockquote) {
  border-left: 3px solid var(--border-strong);
  color: var(--text-3);
  margin: 8px 0;
  padding-left: 12px;
}

.markdown-view :deep(a) {
  color: var(--brand-600);
}

.markdown-view :deep(table) {
  border-collapse: collapse;
  margin: 8px 0;
}

.markdown-view :deep(th),
.markdown-view :deep(td) {
  border: 1px solid var(--border);
  padding: 4px 10px;
}

.markdown-view > :deep(:first-child) {
  margin-top: 0;
}

.markdown-view > :deep(:last-child) {
  margin-bottom: 0;
}
</style>
