<template>
  <div class="chatgpt-settings">
    <div class="section-header">
      <h2>{{ activeAccountTab === 'server' && codexVisible ? t('codexSettings.title') : t('chatgptSettings.title') }}</h2>
      <p class="section-description">{{ activeAccountTab === 'server' && codexVisible ? t('codexSettings.shortDescription') : t('chatgptSettings.description') }}</p>
    </div>

    <div v-if="loading" class="chatgpt-settings__loading">
      <t-loading :text="t('chatgptSettings.loading')" />
    </div>

    <template v-else>
      <t-tabs v-if="codexVisible" v-model="activeAccountTab" class="chatgpt-settings__account-tabs">
        <t-tab-panel value="server" :label="t('codexSettings.title')" />
        <t-tab-panel value="personal" :label="t('chatgptSettings.personalTab')" />
      </t-tabs>

      <div v-show="activeAccountTab === 'personal'">
      <details class="chatgpt-settings__loopback">
        <summary>{{ t('chatgptSettings.loopbackTitle') }}</summary>
        <p>{{ t('chatgptSettings.loopbackHint') }}</p>
      </details>

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
      </div>

      <section v-show="activeAccountTab === 'server'" v-if="codexVisible" class="settings-group chatgpt-settings__codex">
        <t-alert v-if="codexStatusLoadFailed" theme="warning" :title="t('codexSettings.statusLoadFailed')">
          <t-button variant="outline" :loading="codexLoading" @click="loadCodex">{{ t('common.refresh') }}</t-button>
        </t-alert>

        <template v-else-if="codexStatus">
        <div class="chatgpt-settings__overview">
        <div class="setting-row chatgpt-settings__account-status-card">
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

        <section class="chatgpt-settings__usage-card">
          <div class="chatgpt-settings__usage-card-heading">
            <div>
              <h3>{{ t('codexSettings.usageCardTitle') }}</h3>
              <p>{{ t('codexSettings.usageCardHint') }}</p>
            </div>
            <t-button variant="text" size="small" :loading="codexUsageLoading" :disabled="!codexStatus.connected" @click="loadCodexUsage">
              {{ t('common.refresh') }}
            </t-button>
          </div>
          <div v-if="codexUsageLoading" class="chatgpt-settings__usage-unavailable"><t-loading size="small" /></div>
          <t-alert v-else-if="codexUsageLoadFailed" theme="warning" :title="t('codexSettings.usageUnavailable')" />
          <t-empty v-else-if="!codexUsage?.available || codexUsage.limits.length === 0" :description="t('codexSettings.usageUnavailable')" />
          <div v-else class="chatgpt-settings__usage-limits">
            <p v-if="usageUpdatedAtForLocale(codexUsage.updated_at)" class="chatgpt-settings__usage-updated">
              {{ t('codexSettings.usageUpdatedAt', { time: usageUpdatedAtForLocale(codexUsage.updated_at) }) }}
            </p>
            <template v-for="limit in codexUsage.limits" :key="limit.id || limit.name">
              <div v-for="window in limit.windows.filter(usableUsageWindow)" :key="`${limit.id}-${window.kind}`" class="chatgpt-settings__usage-window">
                <div class="chatgpt-settings__usage-window-label">
                  <span>{{ limit.name || limit.id }}</span>
                  <span v-if="usageWindowDurationForLocale(window.window_duration_mins)">{{ usageWindowDurationForLocale(window.window_duration_mins) }}</span>
                </div>
                <div v-if="usagePercent(window.used_percent) !== null" class="chatgpt-settings__usage-bar" role="progressbar" :aria-label="`${limit.name || limit.id} ${t('codexSettings.usageUsed', { percent: usagePercent(window.used_percent) })}`" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="usagePercent(window.used_percent)!">
                  <span :style="{ width: `${usagePercent(window.used_percent)}%` }" />
                </div>
                <div class="chatgpt-settings__usage-window-meta">
                  <span>{{ t('codexSettings.usageUsed', { percent: usagePercent(window.used_percent) ?? t('common.noData') }) }}</span>
                  <span>{{ t('codexSettings.usageRemaining', { percent: usagePercent(window.remaining_percent) ?? t('common.noData') }) }}</span>
                  <span v-if="usageResetAtForLocale(window.resets_at)">{{ t('codexSettings.usageResetsAt', { time: usageResetAtForLocale(window.resets_at) }) }}</span>
                </div>
              </div>
            </template>
          </div>
        </section>

        </div>

        <template v-if="codexStatus.connected">
          <div v-if="codexModelsLoading" class="chatgpt-settings__models-loading">
            <t-loading :text="t('codexSettings.modelsLoading')" size="small" />
          </div>
          <t-alert v-else-if="codexModelsLoadFailed" theme="warning" :title="t('codexSettings.modelsLoadFailed')">
            <t-button variant="outline" @click="loadCodexModels">{{ t('common.refresh') }}</t-button>
          </t-alert>
          <t-empty v-else-if="codexModels.length === 0" :description="t('codexSettings.modelsEmpty')" />
          <div v-else class="chatgpt-settings__model-list chatgpt-settings__model-grid">
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
            <p class="chatgpt-settings__grant-warning"><t-icon name="info-circle" />
              {{ t('codexSettings.grantsUsageWarning') }}
            </p>
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
              <div v-else class="chatgpt-settings__grant-groups">
                <section v-for="group in codexGrantGroups" :key="group.type" class="chatgpt-settings__grant-group">
                  <h5>{{ group.label }} <span>{{ group.models.length }}</span></h5>
                  <div class="chatgpt-settings__grant-list">
                    <div v-for="model in group.models" :key="model.id" class="chatgpt-settings__grant-row"
                      :class="{ 'is-selected': selectedCodexGrantIDs.includes(model.id) }">
                      <div class="chatgpt-settings__grant-model">
                        <div class="chatgpt-settings__model-name">{{ model.display_name || model.name }}</div>
                        <code>{{ model.name }}</code>
                      </div>
                      <t-checkbox :model-value="selectedCodexGrantIDs.includes(model.id)"
                        :aria-label="`${model.display_name || model.name} · ${group.label} · ${t('codexSettings.grantModel')}`"
                        :disabled="savingCodexGrants"
                        @change="(checked: boolean) => toggleCodexGrant(model.id, checked)" />
                    </div>
                  </div>
                </section>
              </div>
              <div class="chatgpt-settings__grant-actions">
                <t-button theme="primary" :disabled="!grantMemberID || savingCodexGrants" :loading="savingCodexGrants" @click="saveCodexGrants">
                  {{ t('codexSettings.grantsSave') }}
                </t-button>
              </div>
            </template>
          </section>

        <p class="chatgpt-settings__capability"><t-icon name="info-circle" />
          {{ t('codexSettings.capabilityHint') }}
        </p>
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
  getCodexUsage,
  listCodexGrantUsers,
  listCodexModelGrants,
  listCodexModels,
  selectCodexModel,
  updateCodexUserGrants,
  type CodexConnectionStatus,
  type CodexModel,
  type CodexModelGrant,
  type CodexGrantUser,
  type CodexUsage,
} from '@/api/codex'
import { usagePercent, usageResetAt, usageUpdatedAt, usageWindowDuration, usableUsageWindow } from '@/api/codexUsage'
import { useAuthStore } from '@/stores/auth'
import { useChatResourcesStore } from '@/stores/chatResources'
import { grantSelectionForUser, toggleGrantSelection } from '@/api/codexGrantState'

const { t, locale } = useI18n()
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
const codexUsage = ref<CodexUsage | null>(null)
const codexUsageLoading = ref(false)
const codexUsageLoadFailed = ref(false)
let codexUsageRequest = 0
const activeAccountTab = ref<'server' | 'personal'>('personal')
let accountTabInitialized = false
const codexGrantsLoading = ref(false)
const codexGrantsLoadFailed = ref(false)
const savingCodexGrants = ref(false)
const codexGrants = ref<CodexModelGrant[]>([])
const grantUsers = ref<CodexGrantUser[]>([])
const grantMemberID = ref('')
const selectedCodexGrantIDs = ref<string[]>([])
const codexGrantGroups = computed(() => [
  { type: 'chat', label: t('codexSettings.chatModel'), models: codexGrants.value.filter(model => model.type !== 'VLLM') },
  { type: 'vision', label: t('codexSettings.visionModel'), models: codexGrants.value.filter(model => model.type === 'VLLM') },
].filter(group => group.models.length > 0))

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
    ? t('codexSettings.sharedDescription')
    : t('codexSettings.notConnectedDescription')
))
const codexVisible = computed(() => !!codexStatus.value?.enabled || codexStatusLoadFailed.value)
const canManageCodexGrants = computed(() => !!codexStatus.value?.manage_grants)

const usageLocale = computed(() => locale.value)
const usageWindowDurationForLocale = (minutes: number | null) => usageWindowDuration(minutes, usageLocale.value)
const usageResetAtForLocale = (seconds: number | null) => usageResetAt(seconds, usageLocale.value)
const usageUpdatedAtForLocale = (value: string | undefined) => usageUpdatedAt(value, usageLocale.value)

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

async function loadCodexUsage() {
  if (!codexStatus.value?.enabled || !codexStatus.value.connected) return
  const request = ++codexUsageRequest
  codexUsageLoading.value = true
  codexUsageLoadFailed.value = false
  try {
    const next = await getCodexUsage()
    if (unmounted || request !== codexUsageRequest) return
    codexUsage.value = next
  } catch {
    if (unmounted || request !== codexUsageRequest) return
    codexUsage.value = null
    codexUsageLoadFailed.value = true
  } finally {
    if (!unmounted && request === codexUsageRequest) codexUsageLoading.value = false
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
    if (!accountTabInitialized) {
      activeAccountTab.value = nextStatus.enabled ? 'server' : 'personal'
      accountTabInitialized = true
    }
    codexStatusLoadFailed.value = false
    if (nextStatus.enabled && nextStatus.connected) {
      await loadCodexModels()
    }
    else {
      codexModels.value = []
      codexUsageRequest++
      codexUsage.value = null
      codexUsageLoadFailed.value = false
      codexUsageLoading.value = false
    }
    if (nextStatus.enabled) await loadCodexGrants()
    if (nextStatus.enabled && nextStatus.connected) void loadCodexUsage()
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

.chatgpt-settings__account-tabs { margin: 0 0 20px; }
.chatgpt-settings__loopback {
  margin: 0 0 16px;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-secondary);

  summary { cursor: pointer; color: var(--td-text-color-primary); }
  p { margin: 8px 0 0; line-height: 1.6; }
}
.chatgpt-settings__account-status-card {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  padding: 16px;
  border: 1px solid color-mix(in srgb, var(--td-brand-color) 28%, var(--td-component-stroke));
  border-radius: 8px;
  background: color-mix(in srgb, var(--td-brand-color) 5%, var(--td-bg-color-container));
}
.chatgpt-settings__account-status-card .setting-info p { margin: 6px 0; }
.chatgpt-settings__usage-card {
  margin-top: 16px;
  padding: 16px;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-container);
}
.chatgpt-settings__usage-card-heading { display: flex; justify-content: space-between; gap: 16px; align-items: start; }
.chatgpt-settings__usage-card-heading h3 { margin: 0 0 4px; font-size: 15px; }
.chatgpt-settings__usage-card-heading p { margin: 0; color: var(--td-text-color-secondary); line-height: 1.5; }
.chatgpt-settings__usage-unavailable { padding: 16px 0; }
.chatgpt-settings__usage-limits { display: grid; gap: 14px; margin-top: 16px; }
.chatgpt-settings__usage-updated { margin: 0; color: var(--td-text-color-secondary); font-size: 12px; }
.chatgpt-settings__usage-window { padding-top: 12px; border-top: 1px solid var(--td-component-stroke); }
.chatgpt-settings__usage-window-label, .chatgpt-settings__usage-window-meta { display: flex; justify-content: space-between; gap: 10px; }
.chatgpt-settings__usage-window-label { margin-bottom: 8px; font-weight: 500; }
.chatgpt-settings__usage-window-meta { margin-top: 7px; flex-wrap: wrap; color: var(--td-text-color-secondary); font-size: 12px; }
.chatgpt-settings__usage-bar { height: 8px; overflow: hidden; border-radius: 999px; background: var(--td-bg-color-secondarycontainer); }
.chatgpt-settings__usage-bar span { display: block; height: 100%; border-radius: inherit; background: var(--td-brand-color); transition: width .2s ease; }

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

.chatgpt-settings__model-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 250px), 1fr));
  gap: 10px;
  border: 0;
  margin-top: 16px;

  .chatgpt-settings__model-row {
    border: 1px solid var(--td-component-stroke);
    border-radius: 6px;
    background: var(--td-bg-color-container);
  }
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

.chatgpt-settings__overview {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
  .chatgpt-settings__usage-card { margin-top: 0; }
  .chatgpt-settings__account-status-card { align-items: flex-start; flex-direction: column; }
  .chatgpt-settings__alert { grid-column: 1 / -1; }
}
.chatgpt-settings__grant-warning, .chatgpt-settings__capability {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  font-size: 12px;
  line-height: 1.6;
  color: var(--td-text-color-secondary);
  .t-icon { flex-shrink: 0; margin-top: 3px; }
}
.chatgpt-settings__grant-warning { margin: 0 0 20px; }
.chatgpt-settings__capability { margin: 12px 0 0; }
.chatgpt-settings__grant-member { width: min(100%, 440px); margin-bottom: 20px; }
.chatgpt-settings__grant-placeholder { color: var(--td-text-color-secondary); padding: 16px 0; }
.chatgpt-settings__grant-group h5 {
  display: flex; align-items: center; gap: 8px;
  margin: 0 0 10px; font-size: 13px; font-weight: 500;
  span { color: var(--td-text-color-placeholder); font-size: 12px; font-weight: 400; }
}
.chatgpt-settings__grant-groups { display: grid; gap: 20px; }
.chatgpt-settings__grant-list {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
}
.chatgpt-settings__grant-row {
  display: flex; justify-content: space-between; align-items: center;
  gap: 12px; padding: 12px 14px;
  border: 1px solid var(--td-component-stroke); border-radius: 8px;
  transition: background .15s, border-color .15s;
  &.is-selected { border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
  code { color: var(--td-text-color-secondary); font-size: 12px; overflow-wrap: anywhere; }
  .t-checkbox { flex-shrink: 0; }
}
.chatgpt-settings__grant-model { min-width: 0; }
.chatgpt-settings__grant-actions {
  display: flex; justify-content: flex-end;
  position: sticky; bottom: 0; z-index: 2;
  margin-top: 20px; padding: 14px 0;
  border-top: 1px solid var(--td-component-stroke);
  background: var(--td-bg-color-container);
}
@media (max-width: 860px) {
  .chatgpt-settings__overview { grid-template-columns: minmax(0, 1fr); }
}
@media (max-width: 640px) {
  .chatgpt-settings__grant-list { grid-template-columns: minmax(0, 1fr); }
  .chatgpt-settings__connection-actions,
  .chatgpt-settings__model-row { align-items: flex-start; flex-direction: column; }
}
</style>
