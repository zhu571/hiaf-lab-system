<template>
  <section class="comments">
    <h3>{{ t('issues.comments.title') }}</h3>
    <div v-if="comments.length === 0" class="empty-hint">{{ t('issues.comments.empty') }}</div>
    <div v-for="comment in comments" :key="comment.id" class="comment">
      <span class="avatar">{{ comment.author_id.slice(0, 1).toUpperCase() }}</span>
      <div class="comment-body">
        <div class="comment-meta">
          <!-- 后端 Comment 响应无 display_name 字段（go-server/issues/model.go:38-44 实测），保持 author_id（§3.7/R8 登记） -->
          <strong>{{ comment.author_id }}</strong>
          <span>{{ formatDateTime(comment.created_at) }}</span>
        </div>
        <p>{{ comment.content }}</p>
      </div>
    </div>
    <el-input v-model="content" type="textarea" :rows="3" :placeholder="t('issues.comments.placeholder')" />
    <el-button class="send-btn" type="primary" :disabled="!content.trim()" @click="submit">{{ t('issues.comments.send') }}</el-button>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Comment } from '@/api/issues'
import { formatDateTime } from '@/utils/datetime'

defineProps<{ comments: Comment[] }>()
const emit = defineEmits<{ submit: [content: string] }>()
const content = ref('')

const { t } = useI18n()

function submit() {
  emit('submit', content.value)
  content.value = ''
}
</script>

<style scoped>
.comments {
  display: grid;
  gap: 14px;
}

.comments h3 {
  font-size: 15px;
}

.empty-hint {
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius-md);
  color: var(--text-3);
  padding: 18px;
  text-align: center;
}

.comment {
  border-bottom: 1px dashed var(--border);
  display: flex;
  gap: 10px;
  padding-bottom: 14px;
}

.avatar {
  background: linear-gradient(135deg, var(--brand-500), var(--brand-700));
  border-radius: 50%;
  color: var(--text-inverse);
  display: grid;
  flex-shrink: 0;
  font-size: 13px;
  font-weight: 700;
  height: 32px;
  place-items: center;
  width: 32px;
}

.comment-body {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.comment-meta {
  align-items: baseline;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.comment-meta strong {
  color: var(--text-1);
  font-size: 13px;
}

.comment-meta span {
  color: var(--text-3);
  font-size: 12px;
}

.comment-body p {
  color: var(--text-2);
  overflow-wrap: break-word;
}

.send-btn {
  justify-self: end;
}
</style>
