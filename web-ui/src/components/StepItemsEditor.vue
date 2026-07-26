<template>
  <div class="step-items-editor">
    <el-table :data="rows" size="small">
      <el-table-column label="#" width="46" align="center">
        <template #default="{ $index }">{{ $index + 1 }}</template>
      </el-table-column>
      <el-table-column label="名称" min-width="160">
        <template #default="{ row }">
          <el-input v-model="row.name" placeholder="步骤名称" @input="emitChange" />
        </template>
      </el-table-column>
      <el-table-column label="描述" min-width="180">
        <template #default="{ row }">
          <el-input v-model="row.description" placeholder="可选" @input="emitChange" />
        </template>
      </el-table-column>
      <el-table-column label="依赖" width="130">
        <template #default="{ row, $index }">
          <el-select v-model="row.depends_on_order" clearable placeholder="无" @change="emitChange">
            <el-option v-for="j in $index" :key="j" :label="`${j}. ${rows[j - 1].name || '（未命名）'}`" :value="j" />
          </el-select>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="168">
        <template #default="{ $index }">
          <el-button size="small" text :disabled="$index === 0" @click="move($index, -1)">上移</el-button>
          <el-button size="small" text :disabled="$index === rows.length - 1" @click="move($index, 1)">下移</el-button>
          <el-button size="small" text type="danger" @click="removeRow($index)">删除</el-button>
        </template>
      </el-table-column>
      <template #empty>
        <el-empty description="暂无步骤，请添加" :image-size="48" />
      </template>
    </el-table>
    <el-button class="add-btn" size="small" :disabled="rows.length >= MAX_ITEMS" @click="addRow">添加步骤</el-button>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { StepTemplateItem } from '../api/stepTemplates'

// 与后端 steptemplates.MaxItems 保持一致
const MAX_ITEMS = 30

type EditableStep = {
  name: string
  description: string
  depends_on_order: number | null
}

const props = defineProps<{ modelValue: StepTemplateItem[] }>()
const emit = defineEmits<{ 'update:modelValue': [items: StepTemplateItem[]] }>()

// 只在挂载时拷贝一次 props，之后以本地编辑状态为准；父组件用 :key 控制重新挂载
const rows = ref<EditableStep[]>(
  (props.modelValue ?? [])
    .slice()
    .sort((a, b) => a.step_order - b.step_order)
    .map((s) => ({ name: s.name, description: s.description ?? '', depends_on_order: s.depends_on_order ?? null }))
)

function emitChange() {
  emit(
    'update:modelValue',
    rows.value.map((r, i) => ({
      name: r.name,
      description: r.description || undefined,
      step_order: i + 1,
      depends_on_order: r.depends_on_order
    }))
  )
}

function addRow() {
  rows.value.push({ name: '', description: '', depends_on_order: null })
  emitChange()
}

function removeRow(index: number) {
  const removedOrder = index + 1
  rows.value.splice(index, 1)
  for (const r of rows.value) {
    if (r.depends_on_order === removedOrder) r.depends_on_order = null
    else if (r.depends_on_order !== null && r.depends_on_order > removedOrder) r.depends_on_order -= 1
  }
  emitChange()
}

function move(index: number, dir: number) {
  const target = index + dir
  if (target < 0 || target >= rows.value.length) return
  const [row] = rows.value.splice(index, 1)
  rows.value.splice(target, 0, row)
  // 依赖按序号引用，交换两行的序号引用使其跟随步骤本身
  const a = index + 1
  const b = target + 1
  for (const r of rows.value) {
    if (r.depends_on_order === a) r.depends_on_order = b
    else if (r.depends_on_order === b) r.depends_on_order = a
  }
  emitChange()
}
</script>

<style scoped>
.step-items-editor {
  display: grid;
  gap: 10px;
}

.add-btn {
  justify-self: start;
}
</style>
