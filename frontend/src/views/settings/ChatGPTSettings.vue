<template>
  <div class="chatgpt-settings">
    <div class="section-header">
      <h2>{{ t('chatgptSettings.title') }}</h2>
      <p class="section-description">{{ t('chatgptSettings.description') }}</p>
    </div>

    <div v-if="loading" class="chatgpt-settings__loading">
      <t-loading :text="t('chatgptSettings.loading')" />
    </div>

    <template v-else>
      <t-alert
        theme="info"
        :message="t('chatgptSettings.loopbackHint')"
        class="chatgpt-settings__alert"
      />

      <t-alert
        v-if="status?.pending && status.pending.status !== 'succeeded'"
        :theme="pendingFailed ? 'warning' : 'info'"
        :title="pendingFailed ? t('chatgptSettings.authorizationFailed') : t('chatgptSettings.authorizationPending')"
        :message="pendingFailed ? t('chatgptSettings.authorizationFailedHint') : t('chatgptSettings.authorizationPendingHint')"
        class="chatgpt-settings__alert"
      />

      <t-alert
        v-if="authorizationPopupBlocked && authorizationUrl"
        theme="warning"
        :title="t('chatgptSettings.popupBlocked')"
        class="chatgpt-settings__alert"
      >
        <a :href="authorizationUrl" target="_blank" rel="noopener noreferrer">
          {{ t('chatgptSettings.openAuthorizationLink') }}
        </a>
      </t-alert>

      <t-alert
        v-if="status?.connected && status.plan_enabled === false"
        theme="warning"
        :title="t('chatgptSettings.planUnavailable')"
        :message="t('chatgptSettings.planUnavailableHint')"
        class="chatgpt-settings__alert"
      />

      <section class="settings-group">
        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-label">{{ t('chatgptSettings.connectionTitle') }}</div>
            <p class="desc">{{ connectionDescription }}</p>
            <p v-if="status?.connected && status.email" class="chatgpt-settings__email">
              {{ t('chatgptSettings.connectedAccount', { email: status.email }) }}
            </p>
          </div>
          <div class="setting-control chatgpt-settings__connection-actions">
            <t-tag :theme="status?.connected ? 'success' : 'default'" variant="light">
              {{ status?.connected ? t('chatgptSettings.connected') : t('chatgptSettings.notConnected') }}
            </t-tag>
            <t-button
              v-if="!status?.connected || status.plan_enabled === false"
              theme="primary"
              :loading="authorizing"
              @click="startAuthorization"
            >
              <template #icon><t-icon name="login" /></template>
              {{ t('chatgptSettings.continueWithChatGPT') }}
            </t-button>
            <t-popconfirm
              v-else
              theme="warning"
              :content="t('chatgptSettings.disconnectConfirm')"
              :confirm-btn="{ content: t('chatgptSettings.disconnect'), theme: 'danger' }"
              :cancel-btn="{ content: t('common.cancel') }"
              @confirm="disconnect"
            >
              <t-button theme="danger" variant="outline" :loading="disconnecting">
                {{ t('chatgptSettings.disconnect') }}
              </t-button>
            </t-popconfirm>
          </div>
        </div>
      </section>

      <section class="settings-group chatgpt-settings__models">
        <div class="chatgpt-settings__models-heading">
          <h3>{{ t('chatgptSettings.modelsTitle') }}</h3>
          <p>{{ status?.connected ? t('chatgptSettings.modelsDescription') : t('chatgptSettings.modelsPending') }}</p>
        </div>

        <div v-if="status?.connected && modelsLoading" class="chatgpt-settings__models-loading">
          <t-loading :text="t('chatgptSettings.modelsLoading')" size="small" />
        </div>
        <t-alert v-else-if="status?.connected && modelsLoadFailed" theme="warning" :title="t('chatgptSettings.modelsLoadFailed')">
          <t-button variant="outline" @click="loadModels">{{ t('common.refresh') }}</t-button>
        </t-alert>
        <t-empty v-else-if="status?.connected && models.length === 0" :description="t('chatgptSettings.modelsEmpty')" />
        <div v-else-if="status?.connected" class="chatgpt-settings__model-list">
          <div v-for="model in models" :key="model.slug" class="chatgpt-settings__model-row">
            <div>
              <div class="chatgpt-settings__model-name">{{ model.display_name }}</div>
              <code>{{ model.slug }}</code>
            </div>
            <div class="chatgpt-settings__model-action">
              <t-tag v-if="enabledModelSlugs.has(model.slug)" theme="success" variant="light">
                {{ t('chatgptSettings.modelEnabled') }}
              </t-tag>
              <t-button
                v-else
                theme="primary"
                variant="outline"
                size="small"
                :loading="selectingSlug === model.slug"
                :disabled="!!selectingSlug || status.plan_enabled === false"
                @click="enableModel(model.slug)"
              >
                {{ t('chatgptSettings.enableModel') }}
              </t-button>
            </div>
          </div>
        </div>
      </section>

      <t-alert theme="info" class="chatgpt-settings__usage" :title="t('chatgptSettings.usageTitle')">
        {{ t('chatgptSettings.usageHint') }}
      </t-alert>

      <section v-if="codexVisible" class="settings-group chatgpt-settings__codex">
        <div class="chatgpt-settings__models-heading">
          <h3>{{ t('codexSettings.title') }}</h3>
          <p>{{ t('codexSettings.description') }}</p>
        </div>

        <t-alert v-if="codexStatusLoadFailed" theme="warning" :title="t('codexSettings.statusLoadFailed')">
          <t-button variant="outline" :loading="codexLoading" @click="loadCodex">{{ t('common.refresh') }}</t-button>
        </t-alert>

        <template v-else-if="codexStatus">
        <div class="setting-row">
          <div class="setting-info">
            <div class="setting-label">{{ t('codexSettings.connectionTitle') }}</div>
            <p class="desc">{{ codexConnectionDescription }}</p>
            <p v-if="codexStatus.email" class="chatgpt-settings__email">
              {{ t('codexSettings.connectedAccount', { email: codexStatus.email }) }}
            </p>
            <p v-if="codexStatus.plan" class="desc">{{ t('codexSettings.plan', { plan: codexStatus.plan }) }}</p>
          </div>
          <div class="setting-control chatgpt-settings__connection-actions">
            <t-tag :theme="codexStatus.connected ? 'success' : 'default'" variant="light">
              {{ codexStatus.connected ? t('codexSettings.connected') : t('codexSettings.notConnected') }}
            </t-tag>
            <t-button variant="outline" size="small" :loading="codexLoading" @click="loadCodex">
              {{ t('common.refresh') }}
            </t-button>
          </div>
        </div>

        <template v-if="codexStatus.connected">
          <div v-if="codexModelsLoading" class="chatgpt-settings__models-loading">
            <t-loading :text="t('codexSettings.modelsLoading')" size="small" />
          </div>
          <t-alert v-else-if="codexModelsLoadFailed" theme="warning" :title="t('codexSettings.modelsLoadFailed')">
            <t-button variant="outline" @click="loadCodexModels">{{ t('common.refresh') }}</t-button>
          </t-alert>
          <t-empty v-else-if="codexModels.length === 0" :description="t('codexSettings.modelsEmpty')" />
          <div v-else class="chatgpt-settings__model-list">
            <div v-for="model in codexModels" :key="model.slug" class="chatgpt-settings__model-row">
              <div>
                <div class="chatgpt-settings__model-name">{{ model.display_name }}</div>
                <code>{{ model.slug }}</code>
              </div>
              <div class="chatgpt-settings__model-action">
                <t-tag v-if="enabledCodexModelSlugs.has(model.slug)" theme="success" variant="light">
                  {{ t('codexSettings.modelEnabled') }}
                </t-tag>
                <t-button
                  v-else
                  theme="primary"
                  variant="outline"
                  size="small"
                  :loading="selectingCodexSlug === model.slug"
                  :disabled="!!selectingCodexSlug"
                  @click="enableCodexModel(model.slug)"
                >
                  {{ t('codexSettings.enableModel') }}
                </t-button>
              </div>
            </div>
          </div>
        </template>

        <t-alert theme="info" class="chatgpt-settings__usage" :title="t('codexSettings.usageTitle')">
          {{ t('codexSettings.usageHint') }}
        </t-alert>
        </template>
      </section>

      <t-dialog
        v-model:visible="welcomeDialogVisible"
        :header="t('chatgptSettings.welcomeTitle')"
        :confirm-btn="{ content: t('chatgptSettings.welcomeConfirm') }"
        :cancel-btn="null"
        :close-on-overlay-click="true"
      >
        <p>{{ t('chatgptSettings.welcomeDescription') }}</p>
      </t-dialog>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { useI18n } from 'vue-i18n'
import {
  authorizeChatGPT,
  disconnectChatGPT,
  getChatGPTStatus,
  listChatGPTModels,
  selectChatGPTModel,
  type ChatGPTConnectionStatus,
  type ChatGPTModel,
} from '@/api/chatgpt'
import { chatGPTAuthorizationOutcome } from '@/api/chatgptAuthorization'
import { getCodexStatus, listCodexModels, selectCodexModel, type CodexConnectionStatus, type CodexModel } from '@/api/codex'
import { useChatResourcesStore } from '@/stores/chatResources'

const { t } = useI18n()
const chatResources = useChatResourcesStore()

const loading = ref(true)
const modelsLoading = ref(false)
const modelsLoadFailed = ref(false)
const authorizing = ref(false)
const disconnecting = ref(false)
const selectingSlug = ref('')
const status = ref<ChatGPTConnectionStatus | null>(null)
const models = ref<ChatGPTModel[]>([])
const enabledModelSlugs = ref(new Set<string>())
const authorizationPopupBlocked = ref(false)
const authorizationUrl = ref('')
const welcomeDialogVisible = ref(false)
const codexLoading = ref(false)
const codexStatusLoadFailed = ref(false)
const codexModelsLoading = ref(false)
const codexModelsLoadFailed = ref(false)
const selectingCodexSlug = ref('')
const codexStatus = ref<CodexConnectionStatus | null>(null)
const codexModels = ref<CodexModel[]>([])
const enabledCodexModelSlugs = ref(new Set<string>())

let pollTimer: number | null = null
let authorizationPopup: Window | null = null
let authorizationExpiresAt = 0
let authorizationAttemptID = ''
let pollInFlight = false
let unmounted = false
let welcomeShown = false

const pendingFailed = computed(() => {
  const pendingStatus = status.value?.pending?.status?.toLowerCase() || ''
  return ['failed', 'error', 'expired', 'cancelled', 'canceled'].includes(pendingStatus)
})

const connectionDescription = computed(() => {
  if (status.value?.connected) return t('chatgptSettings.connectedDescription')
  return t('chatgptSettings.notConnectedDescription')
})

const codexConnectionDescription = computed(() => (
  codexStatus.value?.connected
    ? t('codexSettings.connectedDescription')
    : t('codexSettings.notConnectedDescription')
))
const codexVisible = computed(() => !!codexStatus.value?.enabled || codexStatusLoadFailed.value)

async function loadCodexModels() {
  if (!codexStatus.value?.connected) {
    codexModels.value = []
    return
  }
  codexModelsLoading.value = true
  codexModelsLoadFailed.value = false
  try {
    const availableModels = await listCodexModels()
    if (unmounted) return
    codexModels.value = availableModels
    enabledCodexModelSlugs.value = new Set(availableModels.filter((model) => !!model.id).map((model) => model.slug))
  } catch {
    if (unmounted) return
    codexModels.value = []
    codexModelsLoadFailed.value = true
  } finally {
    if (!unmounted) codexModelsLoading.value = false
  }
}

async function loadCodex() {
  codexLoading.value = true
  try {
    const nextStatus = await getCodexStatus()
    if (unmounted) return
    codexStatus.value = nextStatus
    codexStatusLoadFailed.value = false
    if (nextStatus.enabled && nextStatus.connected) await loadCodexModels()
    else codexModels.value = []
  } catch (error: any) {
    if (unmounted) return
    // A 404 means this is an older WeKnora server, so keep the optional entry
    // absent. Other failures must remain actionable for a configured owner.
    if (error?.status === 404) {
      codexStatus.value = null
      codexStatusLoadFailed.value = false
    } else {
      codexStatusLoadFailed.value = true
    }
  } finally {
    if (!unmounted) codexLoading.value = false
  }
}

function stopPolling({ closePopup = false }: { closePopup?: boolean } = {}) {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
  if (closePopup && authorizationPopup && !authorizationPopup.closed) {
    try { authorizationPopup.close() } catch { /* browser may reject cross-window close */ }
  }
  authorizationPopup = null
  authorizationExpiresAt = 0
  authorizationAttemptID = ''
  pollInFlight = false
}

async function loadModels() {
  if (!status.value?.connected) {
    models.value = []
    return
  }
  modelsLoading.value = true
  modelsLoadFailed.value = false
  try {
    const availableModels = await listChatGPTModels()
    if (unmounted) return
    models.value = availableModels
    enabledModelSlugs.value = new Set(
      availableModels.filter((model) => !!model.id).map((model) => model.slug),
    )
  } catch (error: any) {
    if (unmounted) return
    models.value = []
    modelsLoadFailed.value = true
    MessagePlugin.error(t('chatgptSettings.modelsLoadFailed'))
  } finally {
    if (!unmounted) modelsLoading.value = false
  }
}

async function refreshStatus({ loadAvailableModels = true }: { loadAvailableModels?: boolean } = {}) {
  const nextStatus = await getChatGPTStatus()
  if (unmounted) return nextStatus
  status.value = nextStatus
  if (loadAvailableModels && status.value.connected) await loadModels()
  if (!status.value.connected) models.value = []
  return nextStatus
}

async function load() {
  loading.value = true
  try {
    await refreshStatus()
  } catch (error: any) {
    if (unmounted) return
    MessagePlugin.error(error?.message || t('chatgptSettings.loadFailed'))
  } finally {
    if (!unmounted) loading.value = false
  }
}

async function pollAuthorization() {
  if (pollInFlight || !authorizationAttemptID || unmounted) return
  pollInFlight = true
  try {
    const next = await refreshStatus({ loadAvailableModels: false })
    if (unmounted) return
    const outcome = chatGPTAuthorizationOutcome(next, authorizationAttemptID)
    if (outcome === 'succeeded') {
      stopPolling({ closePopup: true })
      await loadModels()
      if (unmounted) return
      if (!welcomeShown) {
        welcomeShown = true
        welcomeDialogVisible.value = true
      }
      MessagePlugin.success(t('chatgptSettings.authorizationSucceeded'))
      return
    }
    if (outcome === 'failed') {
      stopPolling()
      return
    }
    if (authorizationExpiresAt && Date.now() >= authorizationExpiresAt) {
      stopPolling()
      MessagePlugin.warning(t('chatgptSettings.authorizationExpired'))
    }
  } catch {
    // Keep polling through short network interruptions. The next result is authoritative.
  } finally {
    pollInFlight = false
  }
}

async function startAuthorization() {
  stopPolling({ closePopup: true })
  authorizationPopupBlocked.value = false
  authorizationUrl.value = ''
  // Opening first preserves the browser's user-gesture permission; the URL is
  // assigned only after the server creates a short-lived authorization attempt.
  authorizationPopup = window.open('', 'weknora_chatgpt_authorization', 'width=600,height=760')
  authorizing.value = true
  try {
    const authorization = await authorizeChatGPT()
    if (!authorization.authorization_url || !authorization.attempt_id) {
      throw new Error(t('chatgptSettings.authorizeFailed'))
    }
    if (unmounted) return
    authorizationAttemptID = authorization.attempt_id
    authorizationUrl.value = authorization.authorization_url
    if (!authorizationPopup) {
      authorizationPopupBlocked.value = true
      MessagePlugin.warning(t('chatgptSettings.popupBlocked'))
    } else {
      authorizationPopup.location.href = authorization.authorization_url
    }
    const expires = authorization.expires_at ? new Date(authorization.expires_at).getTime() : NaN
    authorizationExpiresAt = Number.isNaN(expires) ? Date.now() + 10 * 60 * 1000 : expires
    await pollAuthorization()
    if (pollTimer === null && authorizationAttemptID) {
      pollTimer = window.setInterval(() => { void pollAuthorization() }, 1500)
    }
  } catch (error: any) {
    if (unmounted) return
    stopPolling({ closePopup: true })
    MessagePlugin.error(error?.message || t('chatgptSettings.authorizeFailed'))
  } finally {
    if (!unmounted) authorizing.value = false
  }
}

async function enableModel(slug: string) {
  selectingSlug.value = slug
  try {
    await selectChatGPTModel(slug)
    if (unmounted) return
    enabledModelSlugs.value = new Set([...enabledModelSlugs.value, slug])
    await chatResources.ensureModels(true)
    if (unmounted) return
    MessagePlugin.success(t('chatgptSettings.modelEnabledSuccess'))
  } catch (error: any) {
    if (unmounted) return
    MessagePlugin.error(error?.message || t('chatgptSettings.modelEnableFailed'))
  } finally {
    if (!unmounted) selectingSlug.value = ''
  }
}

async function disconnect() {
  disconnecting.value = true
  try {
    await disconnectChatGPT()
    if (unmounted) return
    stopPolling({ closePopup: true })
    status.value = { connected: false, loopback_required: true }
    models.value = []
    enabledModelSlugs.value = new Set()
    MessagePlugin.success(t('chatgptSettings.disconnected'))
  } catch (error: any) {
    if (unmounted) return
    MessagePlugin.error(error?.message || t('chatgptSettings.disconnectFailed'))
  } finally {
    if (!unmounted) disconnecting.value = false
  }
}

async function enableCodexModel(slug: string) {
  selectingCodexSlug.value = slug
  try {
    await selectCodexModel(slug)
    if (unmounted) return
    enabledCodexModelSlugs.value = new Set([...enabledCodexModelSlugs.value, slug])
    await chatResources.ensureModels(true)
    if (unmounted) return
    MessagePlugin.success(t('codexSettings.modelEnabledSuccess'))
  } catch (error: any) {
    if (unmounted) return
    MessagePlugin.error(error?.message || t('codexSettings.modelEnableFailed'))
  } finally {
    if (!unmounted) selectingCodexSlug.value = ''
  }
}

onMounted(() => { unmounted = false; void load(); void loadCodex() })
onUnmounted(() => { unmounted = true; stopPolling({ closePopup: true }) })
</script>

<style scoped lang="less">
.chatgpt-settings__loading,
.chatgpt-settings__models-loading {
  display: flex;
  justify-content: center;
  padding: 32px 0;
}

.chatgpt-settings__alert,
.chatgpt-settings__usage {
  margin-bottom: 16px;
}

.chatgpt-settings__connection-actions,
.chatgpt-settings__model-action {
  display: flex;
  align-items: center;
  gap: 10px;
}

.chatgpt-settings__email {
  margin: 6px 0 0;
  color: var(--td-text-color-primary);
}

.chatgpt-settings__models-heading {
  margin-bottom: 12px;

  h3 { margin: 0 0 6px; font-size: 16px; }
  p { margin: 0; color: var(--td-text-color-secondary); }
}

.chatgpt-settings__model-list {
  border: 1px solid var(--td-component-stroke);
  border-radius: 6px;
}

.chatgpt-settings__model-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child { border-bottom: 0; }
  code { color: var(--td-text-color-secondary); font-size: 12px; }
}

.chatgpt-settings__model-name { margin-bottom: 4px; font-weight: 500; }

@media (max-width: 640px) {
  .chatgpt-settings__connection-actions,
  .chatgpt-settings__model-row { align-items: flex-start; flex-direction: column; }
}
</style>
