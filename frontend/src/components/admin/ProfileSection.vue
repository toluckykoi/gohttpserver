<template>
  <div class="profile-section">
    <!-- Profile info card: shows the values fetched from
         /-/api/admin/profile. The version is included so the operator
         doesn't need to scroll to the footer to check it. -->
    <section class="profile-card">
      <h2 class="section-title">Basic Info</h2>
      <dl class="info-grid" v-if="store.profile">
        <div class="info-item">
          <dt>Username</dt>
          <dd>{{ store.profile.username }}</dd>
        </div>
        <div class="info-item">
          <dt>Auth Provider</dt>
          <dd>
            <el-tag size="small" type="info">{{ providerLabel(store.profile.provider) }}</el-tag>
          </dd>
        </div>
        <div class="info-item">
          <dt>Server Version</dt>
          <dd><code>{{ store.profile.version || 'unknown' }}</code></dd>
        </div>
      </dl>
      <el-skeleton v-else :rows="3" animated />
    </section>

    <!-- Change username. The authenticated session is sufficient —
         no current password is required. -->
    <section class="profile-card">
      <h2 class="section-title">Change Username</h2>
      <p class="section-hint">Enter a new username and save.</p>
      <el-form label-position="top" @submit.prevent="handleSubmitUsername">
        <el-form-item label="New Username">
          <el-input
            v-model="newUsername"
            placeholder="Enter new username"
            autocomplete="username"
            :disabled="submittingUsername"
            required
          />
        </el-form-item>
        <div v-if="usernameError" class="form-error">{{ usernameError }}</div>
        <el-button
          type="primary"
          native-type="submit"
          :loading="submittingUsername"
          :disabled="submittingUsername"
        >
          Save Username
        </el-button>
      </el-form>
    </section>

    <!-- Change password. Mirrors the old ChangePasswordDialog flow but
         inlined into the panel. -->
    <section class="profile-card">
      <h2 class="section-title">Change Password</h2>
      <p class="section-hint">New password must be at least 4 characters.</p>
      <el-form label-position="top" @submit.prevent="handleSubmitPassword">
        <el-form-item label="Current Password">
          <el-input
            v-model="oldPassword"
            type="password"
            show-password
            autocomplete="current-password"
            :disabled="submittingPassword"
            required
          />
        </el-form-item>
        <el-form-item label="New Password">
          <el-input
            v-model="newPassword"
            type="password"
            show-password
            autocomplete="new-password"
            :disabled="submittingPassword"
            required
          />
        </el-form-item>
        <el-form-item label="Confirm New Password">
          <el-input
            v-model="confirmPassword"
            type="password"
            show-password
            autocomplete="new-password"
            :disabled="submittingPassword"
            required
          />
        </el-form-item>
        <div v-if="passwordError" class="form-error">{{ passwordError }}</div>
        <el-button
          type="primary"
          native-type="submit"
          :loading="submittingPassword"
          :disabled="submittingPassword"
        >
          Save Password
        </el-button>
      </el-form>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useAdminStore } from '@/stores/adminStore'

const emit = defineEmits<{
  (e: 'username-changed', value: string): void
}>()

const store = useAdminStore()

// ─── Change username state ─────────────────────────────────────────
const newUsername = ref('')
const submittingUsername = ref(false)
const usernameError = ref('')

// ─── Change password state ─────────────────────────────────────────
const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const submittingPassword = ref(false)
const passwordError = ref('')

function providerLabel(provider: string): string {
  switch (provider) {
    case 'login':
      return 'Local'
    case 'openid':
      return 'OpenID'
    case 'oauth2-proxy':
      return 'OAuth2 Proxy'
    case 'github':
      return 'GitHub'
    default:
      return provider || 'Unknown'
  }
}

async function handleSubmitUsername() {
  usernameError.value = ''
  const name = newUsername.value.trim()
  if (!name) {
    usernameError.value = 'Username cannot be empty.'
    return
  }
  if (name.length > 64) {
    usernameError.value = 'Username must be at most 64 characters.'
    return
  }
  submittingUsername.value = true
  try {
    const res = await store.changeUsername(name)
    if (res.ok) {
      ElMessage.success('Username updated')
      newUsername.value = ''
      // Bubble up to App.vue so the header pill updates immediately.
      emit('username-changed', res.data.username)
    } else if (res.error === 'unauthorized') {
      usernameError.value = 'Session expired, please sign in again.'
    } else {
      usernameError.value = res.error || 'Failed to update username.'
    }
  } finally {
    submittingUsername.value = false
  }
}

async function handleSubmitPassword() {
  passwordError.value = ''
  if (!oldPassword.value || !newPassword.value || !confirmPassword.value) {
    passwordError.value = 'All fields are required.'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = 'The two new passwords do not match.'
    return
  }
  if (newPassword.value.length < 4) {
    passwordError.value = 'New password must be at least 4 characters.'
    return
  }
  submittingPassword.value = true
  try {
    const res = await store.changePassword(oldPassword.value, newPassword.value)
    if (res.ok) {
      ElMessage.success('Password updated')
      oldPassword.value = ''
      newPassword.value = ''
      confirmPassword.value = ''
    } else if (res.error === 'unauthorized') {
      passwordError.value = 'Session expired, please sign in again.'
    } else if (res.error === 'invalid current password') {
      passwordError.value = 'Current password is incorrect.'
    } else {
      passwordError.value = res.error || 'Failed to update password.'
    }
  } finally {
    submittingPassword.value = false
  }
}

onMounted(async () => {
  if (!store.profileLoaded) {
    await store.loadProfile()
  }
  // Pre-fill the new-username field with the current value so the
  // operator sees the starting point (but they still have to type the
  // password — we never autofill that).
  if (store.profile) {
    newUsername.value = store.profile.username
  }
})
</script>

<style scoped>
.profile-section {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.profile-card {
  background: #f8fafc;
  border: 1px solid rgba(148, 163, 184, 0.25);
  border-radius: 10px;
  padding: 18px 20px;
}

.section-title {
  margin: 0 0 8px;
  font-size: 15px;
  font-weight: 600;
  color: #0f172a;
}

.section-hint {
  margin: 0 0 14px;
  font-size: 12px;
  color: #64748b;
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 14px;
  margin: 0;
}

.info-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.info-item dt {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #64748b;
  font-weight: 500;
}

.info-item dd {
  margin: 0;
  font-size: 14px;
  color: #0f172a;
  font-weight: 500;
  /* Long usernames / versions shouldn't overflow the card on narrow
     screens. break-word handles the rare very-long-token case. */
  word-break: break-word;
}

.info-item dd code {
  background: rgba(148, 163, 184, 0.2);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, monospace);
  font-size: 12px;
}

.form-error {
  margin: 4px 0 12px;
  padding: 8px 12px;
  background: rgba(239, 68, 68, 0.12);
  border: 1px solid rgba(239, 68, 68, 0.35);
  color: #b91c1c;
  border-radius: 8px;
  font-size: 13px;
}

/* Phone: tighter spacing so the forms fit without excessive scrolling. */
@media (max-width: 640px) {
  .profile-section {
    gap: 14px;
  }
  .profile-card {
    padding: 14px 14px;
  }
  .info-grid {
    /* Stack the info items vertically on very narrow screens — on a
       360px viewport two columns of 180px each don't fit anyway. */
    grid-template-columns: 1fr;
    gap: 10px;
  }
}
</style>
