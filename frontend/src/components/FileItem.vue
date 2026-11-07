<template>
  <div class="file-name-cell">
    <span class="file-icon-chip" :style="chipStyle" aria-hidden="true">
      <svg class="file-icon-glyph" viewBox="0 0 24 24" fill="currentColor">
        <path v-for="(d, i) in iconPaths" :key="i" :d="d" />
      </svg>
    </span>
    <span class="name">{{ file.name }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { FileItem as FileItemType } from '@/types'
import { getFileIconPaths, getFileIconColor } from '@/utils/fileIcon'

interface Props {
  file: FileItemType
}

const props = defineProps<Props>()

const iconPaths = computed(() => getFileIconPaths(props.file.name, props.file.type))
const iconColor = computed(() => getFileIconColor(props.file.name, props.file.type))
/* 渐变底 + 同色细边框：比纯色块更有体积感，且在明暗主题下都成立。 */
const chipStyle = computed(() => ({
  color: iconColor.value,
  background: `linear-gradient(145deg,
    color-mix(in srgb, ${iconColor.value} 18%, transparent),
    color-mix(in srgb, ${iconColor.value} 7%, transparent))`,
  boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${iconColor.value} 14%, transparent)`
}))
</script>

<style scoped>
.file-name-cell {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 28px;
}

.file-icon-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  flex-shrink: 0;
  border-radius: 8px;
  transition: transform var(--transition-base);
}

.file-row:hover .file-icon-chip,
.file-name-cell:hover .file-icon-chip {
  transform: scale(1.08);
}

.file-icon-glyph {
  width: 15px;
  height: 15px;
  display: block;
}

.name {
  font-weight: 500;
  font-size: 14px;
  line-height: 24px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  transition: color var(--transition-base);
}

.name:hover {
  color: var(--el-color-primary);
}
</style>
