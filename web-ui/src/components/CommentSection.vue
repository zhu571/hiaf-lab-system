<template>
  <section class="comments">
    <h3>评论</h3>
    <div v-if="comments.length === 0" class="empty-hint">暂无评论</div>
    <div v-for="comment in comments" :key="comment.id" class="comment">
      <span class="avatar">{{ comment.author_id.slice(0, 1).toUpperCase() }}</span>
      <div class="comment-body">
        <div class="comment-meta">
          <strong>{{ comment.author_id }}</strong>
          <span>{{ new Date(comment.created_at).toLocaleString() }}</span>
        </div>
        <p>{{ comment.content }}</p>
      </div>
    </div>
    <el-input v-model="content" type="textarea" :rows="3" placeholder="添加评论" />
    <el-button class="send-btn" type="primary" :disabled="!content.trim()" @click="submit">发送</el-button>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { Comment } from '../api/issues'

defineProps<{ comments: Comment[] }>()
const emit = defineEmits<{ submit: [content: string] }>()
const content = ref('')

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
  border-radius: 10px;
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
  color: #fff;
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
