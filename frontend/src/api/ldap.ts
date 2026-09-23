import { del, get, post, put } from '@/utils/request'

/**
 * LDAP configuration deliberately omits its bind password. The server returns
 * only `has_bind_password`, which lets the UI explain that a saved secret is
 * retained when the password input is left empty.
 */
export interface LDAPConfig {
  enabled: boolean
  url: string
  bind_dn: string
  base_dn: string
  user_filter: string
  username_attribute: string
  email_attribute: string
  auto_provision: boolean
  start_tls: boolean
  allow_plaintext: boolean
  tls_server_name: string
  tls_ca_pem: string
  has_bind_password: boolean
}

export interface UpdateLDAPConfig extends Omit<LDAPConfig, 'has_bind_password'> {
  /** Omit or send an empty value to retain the existing password. */
  bind_password?: string
}

/** Result returned by a non-persisting LDAP configuration probe. */
export interface LDAPConnectionTestResult {
  ok: boolean
  stage: string
  message: string
}

const endpoint = '/api/v1/system/admin/ldap'

export async function getLDAPConfig(): Promise<LDAPConfig> {
  const response = await get(endpoint)
  return response as unknown as LDAPConfig
}

export async function updateLDAPConfig(config: UpdateLDAPConfig): Promise<LDAPConfig> {
  const response = await put(endpoint, config)
  return response as unknown as LDAPConfig
}

/**
 * Verify the current form values without changing the saved LDAP
 * configuration. An omitted bind password lets the server reuse its saved or
 * environment-provided secret.
 */
export async function testLDAPConnection(config: UpdateLDAPConfig): Promise<LDAPConnectionTestResult> {
  const response = await post(`${endpoint}/test`, config)
  return response as unknown as LDAPConnectionTestResult
}

export async function resetLDAPConfig(): Promise<void> {
  await del(endpoint)
}
