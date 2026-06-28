<template>
  <nav class="breadcrumb" aria-label="Breadcrumb">
    <div class="breadcrumb-inner">
      <button
        class="breadcrumb-item breadcrumb-home"
        type="button"
        @click="navigateTo('/')"
        aria-label="Go to root"
      >
        <el-icon :size="14"><HomeFilled /></el-icon>
        <span>Home</span>
      </button>

      <template v-for="(item, index) in breadcrumb" :key="item.path">
        <span class="breadcrumb-sep" aria-hidden="true">
          <svg viewBox="0 0 16 16" width="10" height="10" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="6 4 10 8 6 12"/>
          </svg>
        </span>
        <button
          class="breadcrumb-item"
          :class="{ 'breadcrumb-item--last': index === breadcrumb.length - 1 }"
          type="button"
          :title="item.name"
          @click="navigateTo(item.path)"
        >
          {{ item.name }}
        </button>
      </template>
    </div>
  </nav>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { HomeFilled } from '@element-plus/icons-vue'
import { parseBreadcrumb } from '@/utils/path'

const fileStore = useFileStore()

const breadcrumb = computed(() => parseBreadcrumb(fileStore.currentPath))

function navigateTo(path: string) {
  fileStore.loadFiles(path)
}
</script>

<style scoped>
.breadcrumb {
  padding: 10px 0 8px;
}

.breadcrumb-inner {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  font-size: 13.5px;
  line-height: 1;
}

.breadcrumb-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  max-width: 220px;
  padding: 5px 10px;
  font-size: 13px;
  font-weight: 500;
  color: var(--el-text-color-secondary);
  background: transparent;
  border: none;
  border-radius: var(--radius-md);
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  transition: background var(--transition-base),
              color var(--transition-base);
}

.breadcrumb-item:hover {
  background: var(--el-fill-color-light);
  color: var(--el-color-primary);
}

.breadcrumb-item:active {
  transform: scale(0.97);
}

.breadcrumb-item--last {
  color: var(--el-text-color-primary);
  font-weight: 600;
  cursor: default;
}

.breadcrumb-item--last:hover {
  background: transparent;
  color: var(--el-text-color-primary);
}

.breadcrumb-home {
  /* The home chip stands out a touch — it's the always-available
     escape hatch to the root. */
  background: color-mix(in srgb, var(--el-fill-color) 50%, transparent);
}

.breadcrumb-home:hover {
  background: color-mix(in srgb, var(--el-color-primary) 12%, transparent);
  color: var(--el-color-primary);
}

.breadcrumb-sep {
  display: inline-flex;
  align-items: center;
  color: var(--el-text-color-placeholder);
  flex-shrink: 0;
  opacity: 0.6;
}

/* Phone: tighten everything */
@media (max-width: 480px) {
  .breadcrumb {
    padding: 8px 0 6px;
  }

  .breadcrumb-item {
    max-width: 140px;
    padding: 4px 8px;
    font-size: 12.5px;
  }

  .breadcrumb-home span {
    /* Hide the "Home" text below 480px to make room for path crumbs.
       Icon alone still telegraphs the action. */
    display: none;
  }

  .breadcrumb-home {
    padding: 6px;
  }
}
</style>