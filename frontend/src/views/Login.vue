<template>
  <div class="login-shell">
    <form class="login-card" @submit.prevent="handleSubmit" autocomplete="on">
      <div class="login-brand">
        <span class="login-logo" aria-hidden="true">
          <img src="/favicon.png" alt="logo" width="22" height="22" style="border-radius:4px">
        </span>
        <h1 class="login-title">GoHTTPServer</h1>
      </div>
      <p class="login-subtitle">Sign in to manage files.</p>

      <div v-if="errorMessage" class="login-error" role="alert">
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
        />
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
        />
      </div>

      <el-button
        type="primary"
        size="large"
        native-type="submit"
        :loading="submitting"
        class="login-submit"
      >
        Sign in
      </el-button>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { useFileStore } from '@/stores/fileStore'
import { ElMessage } from 'element-plus'

interface Props {
  /** Path the user was originally trying to reach; preserved in the
   *  POST body so the server's 302 redirect lands them back where they
   *  started instead of dumping them at the root. */
  next?: string
}

const props = withDefaults(defineProps<Props>(), {
  next: '/'
})

const fileStore = useFileStore()

const username = ref('')
const password = ref('')
const submitting = ref(false)

// Autofocus the username input on mount so the user can start typing
// immediately. Element Plus wraps the native <input> inside its own
// component, so we use a template ref + nextTick to ensure the inner
// input has been rendered before calling .focus().
const usernameInputRef = ref<{ focus?: () => void; input?: HTMLInputElement } | null>(null)
onMounted(() => {
  nextTick(() => {
    // Prefer the exposed focus() helper (handles internal wrapper),
    // but fall back to the raw input node if absent.
    const el = usernameInputRef.value as any
    if (!el) return
    if (typeof el.focus === 'function') {
      el.focus()
      return
    }
    el.input?.focus?.()
  })
})

// Surfaces either a server-supplied error code (mapped to a friendly
// message) or any client-side validation. Empty string hides the alert.
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
// If the server redirected to /-/login?error=... (e.g. after a failed
// login attempt via the backend redirect flow), surface the error.
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
      // The SPA doesn't ship a router — the original App.vue navigates
      // via window.location. After login we land the browser back where
      // it was headed (next), or at the root. window.location.replace
      // prevents the login page from cluttering the back stack.
      const target = props.next || '/'
      // Hard navigation so the SPA fully re-mounts with the new session
      // cookie in place; soft re-render would still trip middleware on
      // the first fetchFiles because Pinia hasn't propagated.
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
.login-shell {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100dvh;
  background: linear-gradient(135deg, #f1f5f9 0%, #e2e8f0 100%);
  color: #1e293b;
  font-family:
    -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  padding: 24px;
  box-sizing: border-box;
}

.login-card {
  width: min(380px, 100%);
  background: #ffffff;
  border: 1px solid rgba(148, 163, 184, 0.35);
  border-radius: 14px;
  padding: 32px 28px;
  box-shadow: 0 18px 40px rgba(15, 23, 42, 0.12);
}

.login-brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 2px;
}

.login-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  background: rgba(14, 165, 233, 0.12);
  border-radius: 8px;
}

.login-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  letter-spacing: -0.01em;
  color: #0f172a;
}

.login-subtitle {
  margin: 8px 0 22px;
  color: #64748b;
  font-size: 13px;
}

.login-error {
  background: #fef2f2;
  border: 1px solid #fecaca;
  color: #b91c1c;
  padding: 10px 12px;
  border-radius: 8px;
  font-size: 13px;
  margin-bottom: 16px;
}

.login-field {
  margin-bottom: 14px;
}

.login-field label {
  display: block;
  font-size: 12px;
  font-weight: 500;
  color: #475569;
  margin-bottom: 6px;
}

.login-submit {
  width: 100%;
  margin-top: 12px;
}
</style>
