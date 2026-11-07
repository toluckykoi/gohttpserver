<template>
  <div class="login-shell">
    <!-- 环境光斑：跟随当前主题色，纯装饰。 -->
    <div class="login-ambient" aria-hidden="true">
      <span class="ambient-orb ambient-orb--a"></span>
      <span class="ambient-orb ambient-orb--b"></span>
      <span class="ambient-grid"></span>
    </div>

    <form class="login-card" @submit.prevent="handleSubmit" autocomplete="on">
      <div class="login-brand">
        <span class="login-logo" aria-hidden="true">
          <img src="/favicon.png" alt="" width="26" height="26">
        </span>
        <h1 class="login-title">GoHTTPServer</h1>
        <span class="login-badge">File Manager</span>
      </div>
      <p class="login-subtitle">Sign in to manage your files.</p>

      <div v-if="errorMessage" class="login-error" role="alert">
        <svg viewBox="0 0 20 20" width="15" height="15" fill="currentColor" aria-hidden="true">
          <path fill-rule="evenodd" d="M18 10A8 8 0 1 1 2 10a8 8 0 0 1 16 0Zm-9-4a1 1 0 1 1 2 0v4a1 1 0 1 1-2 0V6Zm2 7a1 1 0 1 1-2 0 1 1 0 0 1 2 0Z" clip-rule="evenodd"/>
        </svg>
        {{ errorMessage }}
      </div>

      <div class="login-field">
        <label for="login-username">Username</label>
        <el-input
          id="login-username"
          ref="usernameInputRef"
          v-model="username"
          size="large"
          placeholder="Enter username"
          autocomplete="username"
          inputmode="text"
          name="username"
          required
        >
          <template #prefix>
            <el-icon :size="15"><User /></el-icon>
          </template>
        </el-input>
      </div>

      <div class="login-field">
        <label for="login-password">Password</label>
        <el-input
          id="login-password"
          v-model="password"
          size="large"
          type="password"
          placeholder="Enter password"
          autocomplete="current-password"
          name="password"
          show-password
          required
        >
          <template #prefix>
            <el-icon :size="15"><Lock /></el-icon>
          </template>
        </el-input>
      </div>

      <el-button
        type="primary"
        size="large"
        native-type="submit"
        :loading="submitting"
        class="login-submit"
      >
        <el-icon v-if="!submitting" :size="15" class="login-submit-icon"><Right /></el-icon>
        {{ submitting ? 'Signing in…' : 'Sign in' }}
      </el-button>
    </form>

    <p class="login-footnote">GoHTTPServer · Self-hosted file sharing</p>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { ElMessage } from 'element-plus'
import { User, Lock, Right } from '@element-plus/icons-vue'

interface Props {
  /** 用户最初想访问的路径。 */
  next?: string
}

const props = withDefaults(defineProps<Props>(), {
  next: '/'
})

const fileStore = useFileStore()

const username = ref('')
const password = ref('')
const submitting = ref(false)

// Element Plus 包装了原生 input，须在 nextTick 后优先调用暴露的 focus()。
const usernameInputRef = ref<{ focus?: () => void; input?: HTMLInputElement } | null>(null)
onMounted(() => {
  nextTick(() => {
    const el = usernameInputRef.value as any
    if (!el) return
    if (typeof el.focus === 'function') {
      el.focus()
      return
    }
    el.input?.focus?.()
  })
})

// 映射服务端错误码；空字符串表示隐藏提示。
const errorMessage = computed(() => {
  if (!errorCode.value) return ''
  switch (errorCode.value) {
    case 'invalid_credentials':
      return 'Username or password is incorrect.'
    case 'missing_credentials':
      return 'Both username and password are required.'
    case 'network':
      return 'Could not reach the server. Check your connection and retry.'
    default:
      return errorCode.value
  }
})
// 服务端通过 ?error= 回传错误码。
const errorCode = ref<string | null>(
  new URLSearchParams(window.location.search).get('error')
)

async function handleSubmit() {
  errorCode.value = null
  if (!username.value.trim() || !password.value) {
    errorCode.value = 'missing_credentials'
    return
  }
  submitting.value = true
  try {
    const res = await fileStore.loginWithCredentials(username.value.trim(), password.value)
    if (res.ok) {
      // 整页跳转，让 SPA 带上新会话 Cookie 重新挂载。
      // dev server 下页面挂在 Vite base 前缀上，须跳回 base+path 才能留在
      // 热更新环境；生产 base 与资产挂载点重合，保持原样跳目录路径。
      const base = import.meta.env.BASE_URL
      const next = props.next || '/'
      const target =
        import.meta.env.DEV && base !== '/'
          ? base.replace(/\/+$/, '') + (next.startsWith('/') ? next : '/' + next)
          : next
      window.location.replace(target)
    } else {
      errorCode.value = res.error || 'invalid_credentials'
      password.value = ''
    }
  } catch (err) {
    console.error('login submit failed', err)
    ElMessage.error('Sign-in failed unexpectedly')
    errorCode.value = 'network'
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
/* 全部取当前主题的 Element Plus 变量，五个主题（含暗色）自动适配。 */
.login-shell {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 18px;
  min-height: 100dvh;
  padding: 32px 24px;
  box-sizing: border-box;
  overflow: hidden;
  background:
    linear-gradient(180deg, var(--el-bg-color) 0%, var(--el-bg-color-page) 100%);
  color: var(--el-text-color-primary);
  font-family: var(--font-sans);
}

/* ---- 环境光斑 ---- */
.login-ambient {
  position: absolute;
  inset: 0;
  pointer-events: none;
  overflow: hidden;
}

.ambient-orb {
  position: absolute;
  border-radius: 50%;
  filter: blur(80px);
  opacity: 0.5;
}

.ambient-orb--a {
  width: 520px;
  height: 520px;
  top: -180px;
  left: 50%;
  transform: translateX(-78%);
  background: radial-gradient(circle at 50% 50%,
    color-mix(in srgb, var(--el-color-primary) 22%, transparent) 0%,
    transparent 70%);
}

.ambient-orb--b {
  width: 420px;
  height: 420px;
  bottom: -160px;
  left: 50%;
  transform: translateX(2%);
  background: radial-gradient(circle at 50% 50%,
    color-mix(in srgb, var(--el-color-primary) 14%, transparent) 0%,
    transparent 70%);
  opacity: 0.6;
}

/* 细点阵，给纯色背景一点材质感。 */
.ambient-grid {
  position: absolute;
  inset: 0;
  background-image: radial-gradient(
    color-mix(in srgb, var(--el-text-color-placeholder) 22%, transparent) 1px,
    transparent 1px
  );
  background-size: 26px 26px;
  mask-image: radial-gradient(720px 420px at 50% 42%, #000 0%, transparent 75%);
  -webkit-mask-image: radial-gradient(720px 420px at 50% 42%, #000 0%, transparent 75%);
  opacity: 0.5;
}

@media (prefers-reduced-transparency: reduce) {
  .ambient-orb,
  .ambient-grid {
    display: none;
  }

  .login-card {
    background: var(--el-bg-color);
    backdrop-filter: none;
    -webkit-backdrop-filter: none;
  }
}

/* ---- 卡片 ---- */
.login-card {
  position: relative;
  z-index: 1;
  width: min(400px, 100%);
  background: color-mix(in srgb, var(--el-bg-color) 88%, transparent);
  border: 1px solid color-mix(in srgb, var(--el-border-color) 70%, transparent);
  border-radius: var(--radius-xl);
  padding: 36px 32px 32px;
  box-shadow: var(--shadow-xl);
  backdrop-filter: blur(18px) saturate(160%);
  -webkit-backdrop-filter: blur(18px) saturate(160%);
  animation: login-card-in 480ms var(--ease-out) both;
}

@keyframes login-card-in {
  from { opacity: 0; transform: translateY(14px) scale(0.985); }
  to   { opacity: 1; transform: translateY(0) scale(1); }
}

@media (prefers-reduced-motion: reduce) {
  .login-card { animation: none; }
}

.login-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}

.login-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  flex-shrink: 0;
  border-radius: 12px;
  background:
    linear-gradient(145deg,
      color-mix(in srgb, var(--el-color-primary) 18%, transparent),
      color-mix(in srgb, var(--el-color-primary) 6%, transparent));
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 18%, transparent);
  box-shadow: 0 4px 12px color-mix(in srgb, var(--el-color-primary) 14%, transparent);
}

.login-logo img {
  border-radius: 6px;
}

.login-title {
  margin: 0;
  font-size: 21px;
  font-weight: 700;
  letter-spacing: -0.02em;
  color: var(--el-text-color-primary);
}

.login-badge {
  margin-left: auto;
  padding: 3px 9px;
  font-size: 10.5px;
  font-weight: 600;
  letter-spacing: 0.07em;
  text-transform: uppercase;
  color: var(--el-color-primary);
  background: color-mix(in srgb, var(--el-color-primary) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--el-color-primary) 16%, transparent);
  border-radius: var(--radius-pill);
  white-space: nowrap;
}

.login-subtitle {
  margin: 0 0 24px;
  color: var(--el-text-color-secondary);
  font-size: 13.5px;
  line-height: 1.5;
}

.login-error {
  display: flex;
  align-items: center;
  gap: 8px;
  background: color-mix(in srgb, var(--el-color-danger) 8%, var(--el-bg-color));
  border: 1px solid color-mix(in srgb, var(--el-color-danger) 25%, transparent);
  color: var(--el-color-danger);
  padding: 10px 12px;
  border-radius: var(--radius-md);
  font-size: 13px;
  line-height: 1.45;
  margin-bottom: 18px;
  animation: login-shake 360ms var(--ease-out);
}

@keyframes login-shake {
  0%, 100% { transform: translateX(0); }
  30% { transform: translateX(-4px); }
  60% { transform: translateX(3px); }
}

.login-error svg { flex-shrink: 0; }

.login-field {
  margin-bottom: 16px;
}

.login-field label {
  display: block;
  font-size: 12.5px;
  font-weight: 600;
  letter-spacing: 0.01em;
  color: var(--el-text-color-regular);
  margin-bottom: 7px;
}

.login-field :deep(.el-input__wrapper) {
  padding: 4px 14px;
  border-radius: var(--radius-md);
  background: color-mix(in srgb, var(--el-fill-color) 55%, var(--el-bg-color));
  box-shadow: 0 0 0 1px color-mix(in srgb, var(--el-border-color) 80%, transparent) inset !important;
  transition: box-shadow var(--transition-base),
              background var(--transition-base);
}

.login-field :deep(.el-input__wrapper:hover) {
  box-shadow: 0 0 0 1px var(--el-border-color) inset !important;
}

.login-field :deep(.el-input__wrapper.is-focus) {
  background: var(--el-bg-color);
  box-shadow:
    0 0 0 1.5px var(--el-color-primary) inset,
    0 0 0 4px color-mix(in srgb, var(--el-color-primary) 12%, transparent) !important;
}

.login-field :deep(.el-input__prefix) {
  color: var(--el-text-color-placeholder);
}

/* ---- 提交按钮 ---- */
.login-submit {
  width: 100%;
  margin-top: 10px;
  height: 42px;
  font-size: 14.5px;
  font-weight: 600;
  letter-spacing: 0.01em;
  border: none;
  border-radius: var(--radius-md);
  background: linear-gradient(135deg,
    color-mix(in srgb, var(--el-color-primary) 92%, white),
    var(--el-color-primary));
  box-shadow:
    0 1px 2px color-mix(in srgb, var(--el-color-primary) 25%, transparent),
    0 6px 18px color-mix(in srgb, var(--el-color-primary) 22%, transparent);
  transition: transform var(--transition-fast),
              box-shadow var(--transition-base),
              filter var(--transition-base);
}

.login-submit:hover {
  transform: translateY(-1px);
  filter: brightness(1.05);
  box-shadow:
    0 2px 4px color-mix(in srgb, var(--el-color-primary) 28%, transparent),
    0 10px 26px color-mix(in srgb, var(--el-color-primary) 30%, transparent);
}

.login-submit:active {
  transform: translateY(0) scale(0.99);
}

.login-submit-icon {
  margin-right: 2px;
}

.login-footnote {
  position: relative;
  z-index: 1;
  margin: 0;
  font-size: 12px;
  letter-spacing: 0.02em;
  color: var(--el-text-color-placeholder);
}

@media (max-width: 480px) {
  .login-shell { padding: 24px 16px; }
  .login-card { padding: 28px 22px 24px; }
  .login-badge { display: none; }
}
</style>
