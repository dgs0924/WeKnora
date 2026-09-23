<template>
  <div class="ldap-settings">
    <div class="section-header">
      <div class="ldap-settings__heading">
        <div>
          <h2>{{ t('ldapSettings.title') }}</h2>
          <p class="section-description">{{ t('ldapSettings.description') }}</p>
        </div>
        <t-tag :theme="form.enabled ? 'success' : 'default'" variant="light">
          {{ form.enabled ? t('ldapSettings.statusEnabled') : t('ldapSettings.statusDisabled') }}
        </t-tag>
      </div>
    </div>

    <div v-if="loading" class="ldap-settings__loading">
      <t-loading :text="t('ldapSettings.loading')" />
    </div>

    <template v-else>
      <t-alert theme="info" :message="t('ldapSettings.securityHint')" class="ldap-settings__alert" />

      <t-form :data="form" label-align="top" class="ldap-settings__form" @submit="save">
        <section class="settings-group">
          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-label">{{ t('ldapSettings.enabled') }}</div>
              <p class="desc">{{ t('ldapSettings.enabledDescription') }}</p>
            </div>
            <div class="setting-control">
              <t-switch v-model="form.enabled" />
            </div>
          </div>

          <div class="setting-row">
            <div class="setting-info">
              <div class="setting-label">{{ t('ldapSettings.autoProvision') }}</div>
              <p class="desc">{{ t('ldapSettings.autoProvisionDescription') }}</p>
            </div>
            <div class="setting-control">
              <t-switch v-model="form.auto_provision" :disabled="!form.enabled" />
            </div>
          </div>
        </section>

        <section class="settings-group">
          <h3 class="ldap-settings__group-title">{{ t('ldapSettings.connectionTitle') }}</h3>
          <t-form-item :label="t('ldapSettings.url')">
            <t-input v-model="form.url" :placeholder="t('ldapSettings.urlPlaceholder')" :disabled="saving" />
          </t-form-item>
          <t-form-item :label="t('ldapSettings.startTLS')">
            <div class="ldap-settings__switch-field">
              <t-switch v-model="form.start_tls" :disabled="saving" />
              <span>{{ t('ldapSettings.startTLSDescription') }}</span>
            </div>
          </t-form-item>
        </section>

        <section class="settings-group">
          <h3 class="ldap-settings__group-title">{{ t('ldapSettings.searchAccountTitle') }}</h3>
          <t-form-item :label="t('ldapSettings.bindDN')">
            <t-input v-model="form.bind_dn" :placeholder="t('ldapSettings.bindDNPlaceholder')" :disabled="saving" />
          </t-form-item>
          <t-form-item :label="t('ldapSettings.bindPassword')">
            <t-input
              v-model="bindPassword"
              type="password"
              :placeholder="bindPasswordPlaceholder"
              :disabled="saving"
              autocomplete="new-password"
            />
            <div class="ldap-settings__field-hint">{{ t('ldapSettings.bindPasswordHint') }}</div>
          </t-form-item>
        </section>

        <section class="settings-group">
          <h3 class="ldap-settings__group-title">{{ t('ldapSettings.userLookupTitle') }}</h3>
          <t-form-item :label="t('ldapSettings.baseDN')">
            <t-input v-model="form.base_dn" :placeholder="t('ldapSettings.baseDNPlaceholder')" :disabled="saving" />
          </t-form-item>
          <t-form-item :label="t('ldapSettings.userFilter')">
            <t-textarea
              v-model="form.user_filter"
              :placeholder="t('ldapSettings.userFilterPlaceholder')"
              :disabled="saving"
              :autosize="{ minRows: 2, maxRows: 4 }"
            />
            <div class="ldap-settings__field-hint">{{ t('ldapSettings.userFilterHint') }}</div>
          </t-form-item>
          <div class="ldap-settings__attribute-grid">
            <t-form-item :label="t('ldapSettings.usernameAttribute')">
              <t-input v-model="form.username_attribute" :placeholder="t('ldapSettings.usernameAttributePlaceholder')" :disabled="saving" />
            </t-form-item>
            <t-form-item :label="t('ldapSettings.emailAttribute')">
              <t-input v-model="form.email_attribute" :placeholder="t('ldapSettings.emailAttributePlaceholder')" :disabled="saving" />
            </t-form-item>
          </div>
        </section>

        <div class="ldap-settings__actions">
          <t-popconfirm :content="t('ldapSettings.resetConfirm')" @confirm="reset">
            <t-button theme="default" variant="outline" :loading="resetting" :disabled="saving">
              {{ t('ldapSettings.reset') }}
            </t-button>
          </t-popconfirm>
          <t-button theme="primary" type="submit" :loading="saving" :disabled="!canSave">
            {{ saving ? t('ldapSettings.saving') : t('ldapSettings.save') }}
          </t-button>
        </div>
      </t-form>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import {
  getLDAPConfig,
  resetLDAPConfig,
  updateLDAPConfig,
  type LDAPConfig,
  type UpdateLDAPConfig,
} from '@/api/ldap'

const { t } = useI18n()
const loading = ref(true)
const saving = ref(false)
const resetting = ref(false)
const bindPassword = ref('')
const hasBindPassword = ref(false)

const form = reactive<UpdateLDAPConfig>({
  enabled: false,
  url: '',
  bind_dn: '',
  base_dn: '',
  user_filter: '(&(objectClass=person)(|(mail={username})(uid={username})(sAMAccountName={username})))',
  username_attribute: 'uid',
  email_attribute: 'mail',
  auto_provision: false,
  start_tls: false,
})

const canSave = computed(() => !saving.value && !resetting.value)
const bindPasswordPlaceholder = computed(() => hasBindPassword.value
  ? t('ldapSettings.bindPasswordRetained')
  : t('ldapSettings.bindPasswordPlaceholder'))

function assignConfig(config: LDAPConfig) {
  form.enabled = config.enabled
  form.url = config.url || ''
  form.bind_dn = config.bind_dn || ''
  form.base_dn = config.base_dn || ''
  form.user_filter = config.user_filter || ''
  form.username_attribute = config.username_attribute || ''
  form.email_attribute = config.email_attribute || ''
  form.auto_provision = config.auto_provision
  form.start_tls = config.start_tls
  hasBindPassword.value = config.has_bind_password
  bindPassword.value = ''
}

async function load() {
  loading.value = true
  try {
    assignConfig(await getLDAPConfig())
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('ldapSettings.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    const payload: UpdateLDAPConfig = { ...form }
    if (bindPassword.value) payload.bind_password = bindPassword.value
    else delete payload.bind_password
    assignConfig(await updateLDAPConfig(payload))
    MessagePlugin.success(t('ldapSettings.saveSuccess'))
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('ldapSettings.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function reset() {
  resetting.value = true
  try {
    await resetLDAPConfig()
    await load()
    MessagePlugin.success(t('ldapSettings.resetSuccess'))
  } catch (error: any) {
    MessagePlugin.error(error?.message || t('ldapSettings.resetFailed'))
  } finally {
    resetting.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.ldap-settings__heading,
.ldap-settings__actions,
.ldap-settings__switch-field {
  display: flex;
  align-items: center;
}

.ldap-settings__heading {
  justify-content: space-between;
  gap: 16px;
}

.ldap-settings__heading h2 {
  margin: 0;
}

.ldap-settings__loading {
  display: flex;
  min-height: 240px;
  align-items: center;
  justify-content: center;
}

.ldap-settings__alert {
  margin-bottom: 16px;
}

.ldap-settings__form {
  display: grid;
  gap: 16px;
}

.ldap-settings__group-title {
  margin: 0 0 16px;
  font-size: 15px;
  line-height: 24px;
}

.ldap-settings__switch-field {
  gap: 10px;
  min-height: 32px;
  color: var(--td-text-color-secondary);
  font-size: 13px;
}

.ldap-settings__field-hint {
  margin-top: 6px;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  line-height: 18px;
}

.ldap-settings__attribute-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.ldap-settings__actions {
  justify-content: space-between;
  gap: 12px;
}

@media (max-width: 720px) {
  .ldap-settings__attribute-grid {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
