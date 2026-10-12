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
        <t-alert v-if="codexStatus.connection_error" theme="warning" class="chatgpt-settings__alert">
          {{ t('codexSettings.notConnectedDescription') }}
        </t-alert>

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

          <section v-if="canManageCodexGrants" class="chatgpt-settings__grants">
            <div class="chatgpt-settings__models-heading">
              <h4>{{ t('codexSettings.grantsTitle') }}</h4>
              <p>{{ t('codexSettings.grantsDescription') }}</p>
            </div>
            <t-alert theme="warning" class="chatgpt-settings__grant-warning">
              {{ t('codexSettings.grantsUsageWarning') }}
            </t-alert>
            <div v-if="codexGrantsLoading" class="chatgpt-settings__models-loading">
              <t-loading :text="t('codexSettings.grantsLoading')" size="small" />
            </div>
            <t-alert v-else-if="codexGrantsLoadFailed" theme="warning" :title="t('codexSettings.grantsLoadFailed')">
              <t-button variant="outline" @click="loadCodexGrants">{{ t('common.refresh') }}</t-button>
            </t-alert>
            <template v-else>
              <t-select
                v-model="grantMemberID"
                :label="t('codexSettings.grantMember')"
                :placeholder="t('codexSettings.grantMemberPlaceholder')"
                :disabled="grantUsers.length === 0"
                class="chatgpt-settings__grant-member"
              >
                <t-option v-for="user in grantUsers" :key="user.id" :value="user.id" :label="grantUserLabel(user)" />
              </t-select>
              <t-empty v-if="grantUsers.length === 0" :description="t('codexSettings.grantsMembersEmpty')" />
              <div v-else-if="!grantMemberID" class="chatgpt-settings__grant-placeholder">
                {{ t('codexSettings.grantMemberPlaceholder') }}
              </div>
              <div v-else class="chatgpt-settings__grant-list">
                <div v-for="model in codexGrants" :key="model.id" class="chatgpt-settings__grant-row">
                  <div>
                    <div class="chatgpt-settings__model-name">{{ model.display_name || model.name }}</div>
                    <code>{{ model.name }}</code>
                  </div>
                  <t-checkbox
                    :model-value="selectedCodexGrantIDs.includes(model.id)"
                    :disabled="savingCodexGrants"
                    @change="(checked: boolean) => toggleCodexGrant(model.id, checked)"
                  >
                    {{ t('codexSettings.grantModel') }}
                  </t-checkbox>
                </div>
              </div>
              <div class="chatgpt-settings__grant-actions">
                <t-button theme="primary" :loading="savingCodexGrants" @click="saveCodexGrants">
                  {{ t('codexSettings.grantsSave') }}
                </t-button>
              </div>
            </template>
          </section>

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
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
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
import {
  getCodexStatus,
  listCodexGrantUsers,
  listCodexModelGrants,
  listCodexModels,
  selectCodexModel,
  updateCodexUserGrants,
  type CodexConnectionStatus,
  type CodexModel,
  type CodexModelGrant,
  type CodexGrantUser,
} from '@/api/codex'
import { useAuthStore } from '@/stores/auth'
import { useChatResourcesStore } from '@/stores/chatResources'
import { grantSelectionForUser, toggleGrantSelection } from '@/api/codexGrantState'

const { t } = useI18n()
const chatResources = useChatResourcesStore()
const authStore = useAuthStore()

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
const codexGrantsLoading = ref(false)
const codexGrantsLoadFailed = ref(false)
const savingCodexGrants = ref(false)
const codexGrants = ref<CodexModelGrant[]>([])
const grantUsers = ref<CodexGrantUser[]>([])
const grantMemberID = ref('')
const selectedCodexGrantIDs = ref<string[]>([])

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
const canManageCodexGrants = computed(() => !!codexStatus.value?.manage_grants)

watch(grantMemberID, () => syncSelectedCodexGrantIDs())

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

function grantUserLabel(user: CodexGrantUser): string {
  return user.username || user.email || user.id
}

function syncSelectedCodexGrantIDs() {
  selectedCodexGrantIDs.value = grantSelectionForUser(codexGrants.value, grantMemberID.value)
}

function toggleCodexGrant(modelID: string, checked: boolean) {
  selectedCodexGrantIDs.value = toggleGrantSelection(selectedCodexGrantIDs.value, modelID, checked)
}

async function loadCodexGrants() {
  if (!canManageCodexGrants.value) {
    codexGrants.value = []
    grantUsers.value = []
    return
  }
  codexGrantsLoading.value = true
  codexGrantsLoadFailed.value = false
  try {
    const [models, users] = await Promise.all([
      listCodexModelGrants(),
      listCodexGrantUsers(),
    ])
    if (unmounted) return
    codexGrants.value = models
    // The connection owner already has access; grants apply to active users
    // across all spaces, excluding the owner.
    grantUsers.value = users.filter((user) => user.id !== authStore.currentUserId)
    if (!grantUsers.value.some((user) => user.id === grantMemberID.value)) {
      grantMemberID.value = ''
    }
    syncSelectedCodexGrantIDs()
  } catch {
    if (unmounted) return
    codexGrantsLoadFailed.value = true
  } finally {
    if (!unmounted) codexGrantsLoading.value = false
  }
}

async function saveCodexGrants() {
  const userID = grantMemberID.value
  if (!userID) return
  savingCodexGrants.value = true
  try {
    // This endpoint atomically replaces only the selected member's grants;
    // grants for all other members stay intact.
    await updateCodexUserGrants(userID, selectedCodexGrantIDs.value)
    if (unmounted) return
    await loadCodexGrants()
    MessagePlugin.success(t('codexSettings.grantsSaved'))
  } catch (error: any) {
    if (unmounted) return
    MessagePlugin.error(error?.message || t('codexSettings.grantSaveFailed'))
    await loadCodexGrants()
  } finally {
    if (!unmounted) savingCodexGrants.value = false
  }
}

async function loadCodex() {
  codexLoading.value = true
  try {
    const nextStatus = await getCodexStatus()
    if (unmounted) return
    codexStatus.value = nextStatus
    codexStatusLoadFailed.value = false
    if (nextStatus.enabled && nextStatus.connected) {
      await loadCodexModels()
    }
    else codexModels.value = []
    if (nextStatus.enabled) await loadCodexGrants()
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

.chatgpt-settings__grants {
  margin-top: 24px;
  padding-top: 20px;
  border-top: 1px solid var(--td-component-stroke);

  h4 { margin: 0 0 6px; font-size: 15px; }
}

.chatgpt-settings__grant-warning { margin-bottom: 16px; }
.chatgpt-settings__grant-member { width: min(100%, 440px); margin-bottom: 16px; }
.chatgpt-settings__grant-placeholder { color: var(--td-text-color-secondary); padding: 16px 0; }
.chatgpt-settings__grant-list { border: 1px solid var(--td-component-stroke); border-radius: 6px; }
.chatgpt-settings__grant-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--td-component-stroke);

  &:last-child { border-bottom: 0; }
  code { color: var(--td-text-color-secondary); font-size: 12px; }
}

@media (max-width: 640px) {
  .chatgpt-settings__connection-actions,
  .chatgpt-settings__model-row,
  .chatgpt-settings__grant-row { align-items: flex-start; flex-direction: column; }
}
</style>
