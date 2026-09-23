<template>
  <div class="user-profile">
    <div class="section-header">
      <h2>{{ $t('userProfile.title') }}</h2>
      <p class="section-description">{{ $t('userProfile.description') }}</p>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="loading-inline">
      <t-loading size="small" />
      <span>{{ $t('tenant.loadingInfo') }}</span>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="error-inline">
      <t-alert theme="error" :message="error">
        <template #operation>
          <t-button size="small" @click="loadInfo">{{ $t('tenant.retry') }}</t-button>
        </template>
      </t-alert>
    </div>

    <!-- Content -->
    <div v-else class="settings-group">
      <!-- 用户 ID -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('tenant.api.userIdLabel') }}</label>
          <p class="desc">{{ $t('tenant.api.userIdDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ userInfo?.id || '-' }}</span>
        </div>
      </div>

      <!-- 用户名 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('tenant.api.usernameLabel') }}</label>
          <p class="desc">{{ $t('tenant.api.usernameDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ userInfo?.username || '-' }}</span>
        </div>
      </div>

      <!-- 邮箱 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('tenant.api.emailLabel') }}</label>
          <p class="desc">{{ $t('tenant.api.emailDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ userInfo?.email || '-' }}</span>
        </div>
      </div>

      <!-- 注册时间 -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('tenant.api.createdAtLabel') }}</label>
          <p class="desc">{{ $t('tenant.api.createdAtDescription') }}</p>
        </div>
        <div class="setting-control">
          <span class="info-value">{{ formatDate(userInfo?.created_at) }}</span>
        </div>
      </div>

      <!-- LDAP 绑定：仅目录登录启用时显示，需同时验证本地和目录密码。 -->
      <div v-if="ldapEnabled" class="setting-row">
        <div class="setting-info">
          <label>{{ $t('userProfile.ldapLink.label') }}</label>
          <p class="desc">{{ $t(userInfo?.ldap_bound ? 'userProfile.ldapLink.boundDescription' : 'userProfile.ldapLink.description') }}</p>
        </div>
        <div class="setting-control">
          <t-tag v-if="userInfo?.ldap_bound" theme="success" variant="light">
            {{ $t('userProfile.ldapLink.bound') }}
          </t-tag>
          <t-button v-else theme="primary" variant="outline" size="small" @click="ldapLinkPopupVisible = true">
            {{ $t('userProfile.ldapLink.action') }}
          </t-button>
        </div>
      </div>

      <!-- 修改密码：与其它 setting-row 同款只读行 + 编辑入口，表单进原地 popup -->
      <div class="setting-row">
        <div class="setting-info">
          <label>{{ $t('userProfile.changePassword.label') }}</label>
          <p class="desc">
            {{ oidcOnlyLogin
              ? $t('userProfile.changePassword.oidcOnlyDescription')
              : $t('userProfile.changePassword.description') }}
          </p>
        </div>
        <div class="setting-control">
          <template v-if="oidcOnlyLogin">
            <span class="info-value info-value--muted">—</span>
          </template>
          <template v-else>
            <span class="info-value password-mask" aria-hidden="true">••••••••</span>
            <t-popup
              v-model="passwordPopupVisible"
              trigger="click"
              placement="bottom-end"
              destroy-on-close
              overlay-class-name="wk-popover user-profile-password-popup-overlay"
            >
              <t-button
                theme="default"
                variant="text"
                shape="square"
                size="small"
                class="edit-btn"
                :title="$t('userProfile.changePassword.label')"
                :aria-label="$t('userProfile.changePassword.label')"
              >
                <template #icon>
                  <t-icon name="edit" />
                </template>
              </t-button>
              <template #content>
                <div class="password-popup-inner" @click.stop>
                  <div class="password-popup-title">{{ $t('userProfile.changePassword.label') }}</div>
                  <p class="password-popup-hint">{{ $t('userProfile.changePassword.description') }}</p>
                  <t-form
                    ref="passwordFormRef"
                    :data="passwordForm"
                    :rules="passwordRules"
                    label-align="top"
                    class="password-popup-form"
                    @submit.prevent
                  >
                    <t-form-item :label="$t('userProfile.changePassword.currentLabel')" name="oldPassword">
                      <t-input
                        v-model="passwordForm.oldPassword"
                        type="password"
                        autocomplete="current-password"
                        :disabled="passwordSubmitting"
                        :placeholder="$t('userProfile.changePassword.currentPlaceholder')"
                      />
                    </t-form-item>
                    <t-form-item :label="$t('userProfile.changePassword.newLabel')" name="newPassword">
                      <t-input
                        v-model="passwordForm.newPassword"
                        type="password"
                        autocomplete="new-password"
                        :disabled="passwordSubmitting"
                        :placeholder="$t('userProfile.changePassword.newPlaceholder')"
                      />
                    </t-form-item>
                    <t-form-item :label="$t('userProfile.changePassword.confirmLabel')" name="confirmPassword">
                      <t-input
                        v-model="passwordForm.confirmPassword"
                        type="password"
                        autocomplete="new-password"
                        :disabled="passwordSubmitting"
                        :placeholder="$t('userProfile.changePassword.confirmPlaceholder')"
                        @enter="submitPasswordChange"
                      />
                    </t-form-item>
                  </t-form>
                  <div class="password-popup-footer">
                    <t-button
                      variant="outline"
                      :disabled="passwordSubmitting"
                      @click="closePasswordPopup"
                    >
                      {{ $t('common.cancel') }}
                    </t-button>
                    <t-button
                      theme="primary"
                      :loading="passwordSubmitting"
                      @click="submitPasswordChange"
                    >
                      {{ $t('userProfile.changePassword.submit') }}
                    </t-button>
                  </div>
                </div>
              </template>
            </t-popup>
          </template>
        </div>
      </div>
    </div>

    <t-dialog
      v-model:visible="ldapLinkPopupVisible"
      :header="$t('userProfile.ldapLink.title')"
      :footer="false"
      width="min(480px, calc(100vw - 32px))"
      placement="center"
      attach="body"
      :z-index="3100"
      :close-on-overlay-click="!ldapLinkSubmitting"
      :close-on-esc-keydown="!ldapLinkSubmitting"
      :close-btn="!ldapLinkSubmitting"
      destroy-on-close
    >
      <div class="ldap-link-dialog">
        <p class="ldap-link-dialog__hint">{{ $t('userProfile.ldapLink.hint') }}</p>
        <t-form
          ref="ldapLinkFormRef"
          :data="ldapLinkForm"
          :rules="ldapLinkRules"
          label-align="top"
          class="ldap-link-dialog__form"
          @submit.prevent
        >
          <t-form-item :label="$t('userProfile.ldapLink.localPasswordLabel')" name="localPassword">
            <t-input
              v-model="ldapLinkForm.localPassword"
              type="password"
              size="large"
              autocomplete="current-password"
              :disabled="ldapLinkSubmitting"
              :placeholder="$t('userProfile.ldapLink.localPasswordPlaceholder')"
            />
          </t-form-item>
          <t-form-item :label="$t('userProfile.ldapLink.usernameLabel')" name="username">
            <t-input
              v-model="ldapLinkForm.username"
              size="large"
              autocomplete="username"
              :disabled="ldapLinkSubmitting"
              :placeholder="$t('userProfile.ldapLink.usernamePlaceholder')"
            />
          </t-form-item>
          <t-form-item :label="$t('userProfile.ldapLink.ldapPasswordLabel')" name="password">
            <t-input
              v-model="ldapLinkForm.password"
              type="password"
              size="large"
              autocomplete="off"
              :disabled="ldapLinkSubmitting"
              :placeholder="$t('userProfile.ldapLink.ldapPasswordPlaceholder')"
              @enter="submitLDAPLink"
            />
          </t-form-item>
        </t-form>
        <div class="ldap-link-dialog__actions">
          <t-button variant="outline" :disabled="ldapLinkSubmitting" @click="closeLDAPLinkPopup">
            {{ $t('common.cancel') }}
          </t-button>
          <t-button theme="primary" :loading="ldapLinkSubmitting" @click="submitLDAPLink">
            {{ $t('userProfile.ldapLink.submit') }}
          </t-button>
        </div>
      </div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import type { FormInstanceFunctions, FormRule } from 'tdesign-vue-next'
import {
  getCurrentUser,
  getAuthConfig,
  changePassword,
  linkLDAPAccount,
  logout as logoutApi,
  type UserInfo,
} from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from 'vue-i18n'
import { newPasswordRules } from '@/utils/passwordPolicy'

const { t, locale } = useI18n()
const router = useRouter()
const authStore = useAuthStore()

const userInfo = ref<UserInfo | null>(null)
const complexPasswordEnabled = ref(false)
const ldapEnabled = ref(false)
const loading = ref(true)
const error = ref('')

const passwordPopupVisible = ref(false)
const passwordFormRef = ref<FormInstanceFunctions | null>(null)
const passwordSubmitting = ref(false)
const passwordForm = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: '',
})

const ldapLinkPopupVisible = ref(false)
const ldapLinkFormRef = ref<FormInstanceFunctions | null>(null)
const ldapLinkSubmitting = ref(false)
const ldapLinkForm = reactive({
  localPassword: '',
  username: '',
  password: '',
})

const loadPasswordPolicy = async () => {
  try {
    const resp = await getAuthConfig()
    complexPasswordEnabled.value = !!resp.complex_password_enabled
  } catch {
    complexPasswordEnabled.value = false
  }
}

const loadLDAPAvailability = async () => {
  try {
    const resp = await getAuthConfig()
    ldapEnabled.value = resp.ldap_enabled === true
  } catch {
    ldapEnabled.value = false
  }
}

const oidcOnlyLogin = computed(
  () => userInfo.value?.preferences?.oidc_only_login === true,
)

watch(passwordPopupVisible, (open) => {
  if (!open) {
    resetPasswordForm()
    return
  }
  resetPasswordForm()
  void loadPasswordPolicy()
})

watch(ldapLinkPopupVisible, (open) => {
  if (!open) resetLDAPLinkForm()
})

const passwordRules = computed<Record<string, FormRule[]>>(() => ({
  oldPassword: [
    { required: true, message: t('userProfile.changePassword.currentRequired'), type: 'error' },
  ],
  newPassword: newPasswordRules(t, complexPasswordEnabled.value, [
    {
      validator: (val: string) => val !== passwordForm.oldPassword,
      message: t('userProfile.changePassword.sameAsCurrent'),
      type: 'error',
    },
  ]),
  confirmPassword: [
    { required: true, message: t('auth.confirmPasswordRequired'), type: 'error' },
    {
      validator: (val: string) => val === passwordForm.newPassword,
      message: t('auth.passwordMismatch'),
      type: 'error',
      trigger: 'blur',
    },
  ],
}))

const ldapLinkRules = computed<Record<string, FormRule[]>>(() => ({
  localPassword: [
    { required: true, message: t('userProfile.ldapLink.localPasswordRequired'), type: 'error' },
  ],
  username: [
    { required: true, message: t('userProfile.ldapLink.usernameRequired'), type: 'error' },
  ],
  password: [
    { required: true, message: t('userProfile.ldapLink.ldapPasswordRequired'), type: 'error' },
  ],
}))

const loadInfo = async () => {
  try {
    loading.value = true
    error.value = ''
    const resp = await getCurrentUser()
    if ((resp as any).success && resp.data) {
      userInfo.value = resp.data.user
    } else {
      error.value = resp.message || t('tenant.messages.fetchFailed')
    }
  } catch (err: any) {
    error.value = err?.message || t('tenant.messages.networkError')
  } finally {
    loading.value = false
  }
}

const formatDate = (dateStr: string | undefined) => {
  if (!dateStr) return t('tenant.unknown')
  try {
    const d = new Date(dateStr)
    const fmt = new Intl.DateTimeFormat(locale.value || 'zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    })
    return fmt.format(d)
  } catch {
    return t('tenant.formatError')
  }
}

const resetPasswordForm = () => {
  passwordForm.oldPassword = ''
  passwordForm.newPassword = ''
  passwordForm.confirmPassword = ''
  passwordFormRef.value?.clearValidate?.()
}

const closePasswordPopup = () => {
  if (passwordSubmitting.value) return
  passwordPopupVisible.value = false
  resetPasswordForm()
}

const resetLDAPLinkForm = () => {
  ldapLinkForm.localPassword = ''
  ldapLinkForm.username = ''
  ldapLinkForm.password = ''
  ldapLinkFormRef.value?.clearValidate?.()
}

const closeLDAPLinkPopup = () => {
  if (ldapLinkSubmitting.value) return
  ldapLinkPopupVisible.value = false
  resetLDAPLinkForm()
}

const submitLDAPLink = async () => {
  if (ldapLinkSubmitting.value) return
  const result = await ldapLinkFormRef.value?.validate?.()
  if (result !== true) return

  ldapLinkSubmitting.value = true
  try {
    const resp = await linkLDAPAccount({
      local_password: ldapLinkForm.localPassword,
      username: ldapLinkForm.username,
      password: ldapLinkForm.password,
    })
    if (!resp.success) {
      MessagePlugin.error(resp.message || t('userProfile.ldapLink.failed'))
      return
    }
    ldapLinkPopupVisible.value = false
    resetLDAPLinkForm()
    await loadInfo()
    MessagePlugin.success(t('userProfile.ldapLink.success'))
  } finally {
    ldapLinkSubmitting.value = false
  }
}

const submitPasswordChange = async () => {
  if (passwordSubmitting.value) return
  const result = await passwordFormRef.value?.validate?.()
  if (result !== true) return

  passwordSubmitting.value = true
  try {
    const resp = await changePassword({
      old_password: passwordForm.oldPassword,
      new_password: passwordForm.newPassword,
    })
    if (!resp.success) {
      MessagePlugin.error(resp.message || t('userProfile.changePassword.failed'))
      return
    }

    passwordPopupVisible.value = false
    MessagePlugin.success(t('userProfile.changePassword.success'))
    resetPasswordForm()

    // Backend revokes all sessions on success; mirror that locally and
    // force a fresh login with the new credential.
    try {
      await logoutApi()
    } catch {
      /* ignore — local cleanup still proceeds */
    }
    authStore.logout()
    router.push('/login')
  } catch (err: any) {
    MessagePlugin.error(err?.message || t('userProfile.changePassword.failed'))
  } finally {
    passwordSubmitting.value = false
  }
}

onMounted(() => {
  void loadInfo()
  void loadLDAPAvailability()
})
</script>

<style lang="less" scoped>
@import (reference) '@/components/css/settings-section.less';

.user-profile {
  width: 100%;
}

.section-header {
  .settings-section-header();
}

.loading-inline {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 40px 0;
  justify-content: center;
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-base);
}

.error-inline {
  padding: 20px 0;
}

.settings-group {
  display: flex;
  flex-direction: column;
  gap: 0;
}

.setting-row {
  .setting-row();
}

.setting-info {
  .setting-info();
}

.setting-control {
  .setting-control();

  .info-value {
    font-size: var(--app-text-base);
    color: var(--td-text-color-primary);
    text-align: right;
    word-break: break-word;
  }

  .info-value--muted {
    color: var(--td-text-color-placeholder);
  }

  .edit-btn {
    flex-shrink: 0;
  }
}

.password-mask {
  letter-spacing: 0.12em;
  color: var(--td-text-color-secondary);
}

.ldap-link-dialog {
  min-width: 0;
}

.ldap-link-dialog__hint {
  margin: 0 0 22px;
  padding: 12px 14px;
  border-radius: var(--app-radius-md);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);
  font-size: var(--app-text-md);
  line-height: 1.55;
}

.ldap-link-dialog__form {
  :deep(.t-form__item) {
    margin-bottom: 18px;

    &:last-child {
      margin-bottom: 0;
    }
  }
}

.ldap-link-dialog__actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 24px;
  padding-top: 18px;
  border-top: 1px solid var(--td-component-stroke);
}

@media (max-width: 540px) {
  .ldap-link-dialog__actions :deep(.t-button) {
    flex: 1;
  }
}

.password-popup-inner {
  max-width: 100%;
}

.password-popup-title {
  font-size: var(--app-text-lg);
  font-weight: 600;
  color: var(--td-text-color-primary);
  margin: 0 0 8px;
  line-height: 1.35;
}

.password-popup-hint {
  margin: 0 0 12px;
  font-size: var(--app-text-md);
  line-height: 1.55;
  color: var(--td-text-color-secondary);
}

.password-popup-form {
  :deep(.t-form__item) {
    margin-bottom: 14px;

    &:last-child {
      margin-bottom: 4px;
    }
  }
}

.password-popup-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 16px;
}
</style>

<style lang="less">
/* t-popup 挂到 body，需全局样式；z-index 需高于设置全屏遮罩（2000）。 */
.user-profile-password-popup-overlay {
  z-index: 3050 !important;

}

:root[theme-mode='dark'] .user-profile-password-popup-overlay .t-popup__content {
  background: rgba(36, 36, 36, 0.92) !important;
  border-color: rgba(255, 255, 255, 0.08) !important;
  box-shadow:
    0 0 0 0.5px rgba(255, 255, 255, 0.05),
    0 2px 4px rgba(0, 0, 0, 0.12),
    0 8px 32px rgba(0, 0, 0, 0.28) !important;
}
</style>
