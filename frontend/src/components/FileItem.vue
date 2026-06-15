<template>
  <div class="file-name-cell">
    <el-icon :size="20" class="file-icon">
      <component :is="iconComponent" />
    </el-icon>
    <span class="name">{{ file.name }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { FileItem as FileItemType } from '@/types'
import { getFileIcon } from '@/utils/fileIcon'

interface Props {
  file: FileItemType
}

const props = defineProps<Props>()

const iconComponent = computed(() => getFileIcon(props.file.name, props.file.type))
</script>

<style scoped>
.file-name-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
}

.file-icon {
  color: var(--el-color-primary);
  flex-shrink: 0;
}

.name {
  font-weight: 500;
}
</style>
