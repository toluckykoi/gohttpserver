<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="$emit('update:visible', $event)"
    :title="t('webdav.directoryPlaceholder')"
    width="min(560px, 92vw)"
    :close-on-click-modal="false"
    append-to-body
  >
    <div class="picker-body">
      <div class="current-path">
        <span class="path-label">{{ t('webdav.selected') }}</span>
        <code class="path-value">{{ selectedPath || '/' }}</code>
      </div>

      <el-tree
        ref="treeRef"
        :key="treeKey"
        :data="treeData"
        :props="treeProps"
        node-key="path"
        highlight-current
        :expand-on-click-node="false"
        :load="loadNode"
        lazy
        @current-change="handleCurrentChange"
      >
        <template #default="{ node, data: nodeData }">
          <span class="tree-node" @click="selectNode(nodeData)">
            <el-icon class="node-icon"><Folder /></el-icon>
            <span class="node-label">{{ node.label }}</span>
          </span>
        </template>
      </el-tree>
    </div>

    <template #footer>
      <el-button @click="$emit('update:visible', false)">{{ t('common.cancel') }}</el-button>
      <el-button type="primary" @click="confirm">{{ t('common.confirm') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { Folder } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import type { FileItem } from '@/types'

const { t } = useI18n()

interface Props {
  visible: boolean
  /** 初始选中的路径。 */
  modelValue?: string
}
const props = withDefaults(defineProps<Props>(), {
  modelValue: '/',
})
const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'update:modelValue', value: string): void
}>()

interface TreeNode {
  label: string
  path: string
  isLeaf?: boolean
  children?: TreeNode[]
}

const treeRef = ref()
const treeData = ref<TreeNode[]>([])
const treeKey = ref(0)
const selectedPath = ref<string>(props.modelValue || '/')

const treeProps = {
  label: 'label',
  children: 'children',
  isLeaf: 'isLeaf',
}

watch(
  () => props.visible,
  (open) => {
    if (open) {
      selectedPath.value = props.modelValue || '/'
      // 每次打开都重新挂载树，重置节点展开/选中状态。
      treeKey.value++
      treeData.value = [{
        label: t('webdav.root'),
        path: '/',
      }]
      nextTick(() => {
        const rootNode = treeRef.value?.getNode('/')
        if (rootNode) {
          rootNode.expanded = true
        }
      })
    }
  },
)

// 走文件列表 API（?json=true）并只保留子目录。
async function fetchDir(path: string): Promise<FileItem[]> {
  const sep = path.includes('?') ? '&' : '?'
  const url = path + sep + 'json=true'
  const res = await fetch(url, { cache: 'no-store' })
  if (!res.ok) {
    throw new Error(`Failed to list ${path}: ${res.status}`)
  }
  const data = await res.json() as { files: FileItem[] }
  return (data.files || []).filter((f) => f.type === 'dir')
}

// el-tree 懒加载回调：不预先标记叶子，返回空数组时 el-tree 自动视为叶子。
async function loadNode(node: any, resolve: (data: TreeNode[]) => void) {
  const dirPath = node.level === 0 ? '/' : node.data.path
  try {
    const subdirs = await fetchDir(dirPath)
    resolve(subdirs.map((d) => ({
      label: d.name,
      path: d.path,
      isLeaf: false,
    })))
  } catch {
    resolve([])
  }
}

function handleCurrentChange(_data: any, node: any) {
  selectedPath.value = node.data.path
}

function selectNode(data: TreeNode) {
  selectedPath.value = data.path
  treeRef.value?.setCurrentKey(data.path)
}

function confirm() {
  // 规范化：始终以 "/" 开头；除根目录外不带结尾斜杠。
  let p = selectedPath.value || '/'
  if (!p.startsWith('/')) p = '/' + p
  if (p.length > 1 && p.endsWith('/')) p = p.slice(0, -1)
  emit('update:modelValue', p)
  emit('update:visible', false)
}
</script>

<style scoped>
.picker-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.current-path {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: rgba(14, 165, 233, 0.08);
  border-radius: 8px;
}

.path-label {
  font-size: 12px;
  color: #475569;
}

.path-value {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 13px;
  color: #0f172a;
  word-break: break-all;
}

.tree-node {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  cursor: pointer;
}

.node-icon {
  color: #f59e0b;
  font-size: 16px;
}

.node-label {
  font-size: 13px;
}

:deep(.el-tree) {
  max-height: 50vh;
  overflow-y: auto;
  background: transparent;
}

:deep(.el-tree-node__content) {
  height: 32px;
}
</style>
