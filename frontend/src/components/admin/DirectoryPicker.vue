<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="$emit('update:visible', $event)"
    title="选择访问目录"
    width="min(560px, 92vw)"
    :close-on-click-modal="false"
    append-to-body
  >
    <div class="picker-body">
      <!-- 当前选中路径面包屑,让用户清楚选了哪个目录。 -->
      <div class="current-path">
        <span class="path-label">已选：</span>
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
      <el-button @click="$emit('update:visible', false)">取消</el-button>
      <el-button type="primary" @click="confirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
import { Folder } from '@element-plus/icons-vue'
import type { FileItem } from '@/types'

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
// Bumped every time the dialog opens so the <el-tree> fully re-mounts.
// el-tree caches node state by node-key; without a fresh mount the root
// node ("/") keeps its previous "loaded" flag on the 2nd open and never
// re-fetches its children — leaving the tree empty.
const treeKey = ref(0)
// selectedPath 保存用户最后点击的节点。我们不立即 emit —— 只有
// 点「确定」才 emit —— 这样用户在树里浏览时不会改坏表单字段。
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
      // 重新挂载整棵树,保证重新打开时状态干净、重新拉取根目录。
      treeKey.value++
      treeData.value = [{
        label: '根目录',
        path: '/',
      }]
      // 自动展开根节点,让用户打开就能看到根目录下的子目录。
      // 用 nextTick 确保 el-tree 内部节点已经创建。
      nextTick(() => {
        const rootNode = treeRef.value?.getNode('/')
        if (rootNode) {
          rootNode.expanded = true
        }
      })
    }
  },
)

// fetchDir 调用现有的文件列表 API,只返回子目录。
// ?json=true 端点和文件管理器用的是同一个,所以鉴权和中间件
// 行为与主界面完全一致。
async function fetchDir(path: string): Promise<FileItem[]> {
  const sep = path.includes('?') ? '&' : '?'
  const url = path + sep + 'json=true'
  const res = await fetch(url, { cache: 'no-store' })
  if (!res.ok) {
    throw new Error(`Failed to list ${path}: ${res.status}`)
  }
  const data = await res.json() as { files: FileItem[] }
  // 只保留目录,文件不展示。
  return (data.files || []).filter((f) => f.type === 'dir')
}

// loadNode 是 el-tree 的懒加载回调。展开节点时被调用(根节点展开
// 时也会调用一次)。我们暂不预判叶子节点,让 el-tree 在展开时
// 再调用 loadNode,空结果会自动变成叶子。
async function loadNode(node: any, resolve: (data: TreeNode[]) => void) {
  // node.level === 0 对应我们注入的根节点,path 是 "/"。
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
  // 同步高亮,让视觉状态和选中状态一致。
  treeRef.value?.setCurrentKey(data.path)
}

function confirm() {
  // 规范化:始终以 "/" 开头,除根目录外末尾不带 "/"。
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
