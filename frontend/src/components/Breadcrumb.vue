<template>
  <div class="breadcrumb-container">
    <el-breadcrumb separator="/">
      <el-breadcrumb-item @click="navigateTo('/')">
        <el-icon><HomeFilled /></el-icon>
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
</style>