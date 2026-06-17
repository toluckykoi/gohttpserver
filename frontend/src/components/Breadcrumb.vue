<template>
  <div class="breadcrumb-container">
    <el-breadcrumb separator="/" class="breadcrumb-nav">
      <el-breadcrumb-item @click="navigateTo('/')" class="breadcrumb-home">
        <el-icon :size="16"><HomeFilled /></el-icon>
        /home
      </el-breadcrumb-item>
      <el-breadcrumb-item
        v-for="(item, index) in breadcrumb"
        :key="index"
        :to="item.path"
        @click.prevent="navigateTo(item.path)"
      >
        {{ item.name }}
      </el-breadcrumb-item>
    </el-breadcrumb>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { HomeFilled } from '@element-plus/icons-vue'
import { parseBreadcrumb } from '@/utils/path'

const fileStore = useFileStore()

const breadcrumb = computed(() => {
  return parseBreadcrumb(fileStore.currentPath)
})

function navigateTo(path: string) {
  fileStore.loadFiles(path)
}
</script>

<style scoped>
.breadcrumb-container {
  padding: 16px 0;
}

.breadcrumb-nav {
  align-items: center;
}

/* All breadcrumb inner items: uniform flex alignment */
.breadcrumb-nav :deep(.el-breadcrumb__inner) {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-weight: 500;
  transition: color var(--transition-base);
}

.breadcrumb-nav :deep(.el-breadcrumb__inner:hover) {
  color: var(--el-color-primary);
}

/* Cursor on each item */
.breadcrumb-nav :deep(.el-breadcrumb__item) {
  cursor: pointer;
}

/* Mobile: truncate long paths */
@media (max-width: 480px) {
  .breadcrumb-container {
    padding: 12px 0;
  }

  .breadcrumb-nav :deep(.el-breadcrumb__inner) {
    max-width: 120px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
  }
}
</style>