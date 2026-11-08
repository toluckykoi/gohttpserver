<template>
  <div class="profile-section">
    <section class="profile-card">
      <h2 class="section-title">{{ t('profile.basicInfo') }}</h2>
      <dl class="info-grid" v-if="store.profile">
        <div class="info-item">
          <dt>{{ t('profile.username') }}</dt>
          <dd>{{ store.profile.username }}</dd>
        </div>
        <div class="info-item">
          <dt>{{ t('profile.authProvider') }}</dt>
          <dd>
            <el-tag size="small" type="info">{{ providerLabel(store.profile.provider) }}</el-tag>
          </dd>
        </div>
        <div class="info-item">
          <dt>{{ t('profile.serverVersion') }}</dt>
          <dd><code>{{ store.profile.version || t('common.unknown') }}</code></dd>
        </div>
      </dl>
      <el-skeleton v-else :rows="3" animated />
    </section>

    <!-- 语言切换（个人配置 → 首选项）。 -->
    <section class="profile-card">
      <h2 class="section-title">{{ t('profile.language') }}</h2>
      <p class="section-hint">{{ t('profile.languageHint') }}</p>
      <el-radio-group :model-value="currentLocale" @change="handleLocaleChange">
        <el-radio-button value="en">{{ t('profile.languageEnglish') }}</el-radio-button>
        <el-radio-button value="zh">{{ t('profile.languageChinese') }}</el-radio-button>
      </el-radio-group>
    </section>

    <!-- 改用户名无需验证当前密码。 -->
    <section class="profile-card">
      <h2 class="section-title">{{ t('profile.changeUsername') }}</h2>
      <p class="section-hint">{{ t('profile.changeUsernameHint') }}</p>
      <el-form label-position="top" @submit.prevent="handleSubmitUsername">
        <el-form-item :label="t('profile.newUsername')">
          <el-input
            v-model="newUsername"
            :placeholder="t('profile.newUsernamePlaceholder')"
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
          {{ t('profile.saveUsername') }}
        </el-button>
      </el-form>
    </section>

    <section class="profile-card">
      <h2 class="section-title">{{ t('profile.changePassword') }}</h2>
      <p class="section-hint">{{ t('profile.changePasswordHint') }}</p>
      <el-form label-position="top" @submit.prevent="handleSubmitPassword">
        <el-form-item :label="t('profile.currentPassword')">
          <el-input
            v-model="oldPassword"
            type="password"
            show-password
            autocomplete="current-password"
            :disabled="submittingPassword"
            required
          />
        </el-form-item>
        <el-form-item :label="t('profile.newPassword')">
          <el-input
            v-model="newPassword"
            type="password"
            show-password
            autocomplete="new-password"
            :disabled="submittingPassword"
            required
          />
        </el-form-item>
        <el-form-item :label="t('profile.confirmNewPassword')">
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
          {{ t('profile.savePassword') }}
        </el-button>
      </el-form>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useAdminStore } from '@/stores/adminStore'
import { setLocale } from '@/i18n'

const emit = defineEmits<{
  (e: 'username-changed', value: string): void
}>()

const { t, locale } = useI18n()
const currentLocale = ref<string>(String(locale.value || 'en'))

function handleLocaleChange(val: string | number | boolean | undefined) {
  const next = val === 'zh' ? 'zh' : 'en'
  currentLocale.value = next
  setLocale(next)
}

const store = useAdminStore()

const newUsername = ref('')
const submittingUsername = ref(false)
const usernameError = ref('')

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
      return provider || t('common.unknown')
  }
}

async function handleSubmitUsername() {
  usernameError.value = ''
  const name = newUsername.value.trim()
  if (!name) {
    usernameError.value = t('profile.usernameEmpty')
    return
  }
  if (name.length > 64) {
    usernameError.value = t('profile.usernameTooLong')
    return
  }
  submittingUsername.value = true
  try {
    const res = await store.changeUsername(name)
    if (res.ok) {
      ElMessage.success(t('profile.usernameUpdated'))
      newUsername.value = ''
      emit('username-changed', res.data.username)
    } else if (res.error === 'unauthorized') {
      usernameError.value = t('profile.sessionExpired')
    } else {
      usernameError.value = res.error || t('profile.usernameUpdateFailed')
    }
  } finally {
    submittingUsername.value = false
  }
}

async function handleSubmitPassword() {
  passwordError.value = ''
  if (!oldPassword.value || !newPassword.value || !confirmPassword.value) {
    passwordError.value = t('profile.allFieldsRequired')
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    passwordError.value = t('profile.passwordMismatch')
    return
  }
  if (newPassword.value.length < 4) {
    passwordError.value = t('profile.passwordTooShort')
    return
  }
  submittingPassword.value = true
  try {
    const res = await store.changePassword(oldPassword.value, newPassword.value)
    if (res.ok) {
      ElMessage.success(t('profile.passwordUpdated'))
      oldPassword.value = ''
      newPassword.value = ''
      confirmPassword.value = ''
    } else if (res.error === 'unauthorized') {
      passwordError.value = t('profile.sessionExpired')
    } else if (res.error === 'invalid current password') {
      passwordError.value = t('profile.passwordIncorrect')
    } else {
      passwordError.value = res.error || t('profile.passwordUpdateFailed')
    }
  } finally {
    submittingPassword.value = false
  }
}

onMounted(async () => {
  if (!store.profileLoaded) {
    await store.loadProfile()
  }
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

@media (max-width: 640px) {
  .profile-section {
    gap: 14px;
  }
  .profile-card {
    padding: 14px 14px;
  }
  .info-grid {
    grid-template-columns: 1fr;
    gap: 10px;
  }
}
</style>