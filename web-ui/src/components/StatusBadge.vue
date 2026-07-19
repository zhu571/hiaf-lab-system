<template>
  <el-tag :type="type" size="small" round effect="light">{{ label }}</el-tag>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ value: string }>()

const type = computed(() => {
  if (['active', 'published', 'confirmed', 'resolved'].includes(props.value)) return 'success'
  if (['draft', 'candidate', 'open'].includes(props.value)) return 'warning'
  if (['archived', 'closed', 'locked'].includes(props.value)) return 'info'
  return 'primary'
})

const label = computed(() => props.value.replace(/_/g, ' '))
</script>
