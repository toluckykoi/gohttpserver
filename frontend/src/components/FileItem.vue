<template>
  <div class="file-name-cell">
    <!-- Tinted chip: 12% of the type color behind a full-strength
         glyph. Gives instant type recognition (amber folder, violet
         image, rose video…) without a rainbow of raw icon colors. -->
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
const chipStyle = computed(() => ({
  color: iconColor.value,
  background: `color-mix(in srgb, ${iconColor.value} 12%, transparent)`
}))
</script>

<style scoped>
.file-name-cell {
  display: flex;
  align-items: center;
  gap: 10px;
  /* Match the cell's 24px line-height so the row stays at 41px
     regardless of the chip's intrinsic height. */
  height: 24px;
}

.file-icon-chip {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  border-radius: 7px;
  transition: transform var(--transition-base);
}

.file-icon-glyph {
  width: 14px;
  height: 14px;
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
