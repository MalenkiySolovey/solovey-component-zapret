import api from '@solovey/component-ui/api'

export type NativeListField = 'hostList' | 'hostListExclude' | 'ipSet' | 'ipSetExclude'
export type ListKind = 'hostList' | 'ipSet'
export type NamedList = { kind: ListKind; entries: string[] }
export type LuaOrigin = 'administrator' | 'zapretgui' | 'external'
export type LuaDraft = { origin: LuaOrigin; sourceRef?: string; source: string; imported?: boolean }
export type LuaReference = { id: string; revision: string; approveImported?: boolean }
export type LuaDraftView = { revision: string; drafts: Record<string, LuaDraft> | null; draftRevisions?: Record<string, string>; revisionSources?: Record<string, LuaDraft> }
export type ProfileListRefs = Partial<Record<NativeListField, string>>
export type ListRefs = Record<string, ProfileListRefs>
export type HTTPAction = { name: 'http_domcase' | 'http_hostcase'; spell?: string; outRange?: string }
export type PortInterval = { first: number; last: number }
export type PresetOrigin = { format: string; version: number; engine: string; fileName?: string; sha256: string;
  templateId?: string; referenceVersion?: string; referenceCommit?: string }

export interface Profile {
  id: string
  protocol: 'tcp' | 'udp'
  family?: 'ipv4' | 'ipv6'
  ports: number[] | null
  udpPorts?: number[]
  portRanges?: PortInterval[]
  udpPortRanges?: PortInterval[]
  payload?: 'tls_client_hello' | 'http_req' | 'quic_initial'
  splitAt?: number
  circular?: { failures: number; splits: number[] }
  httpActions?: HTTPAction[]
  quicRepeats?: number
  quicBlobRef?: string
  outRange?: string
  lists?: Partial<Record<NativeListField, string[]>>
}
export function portText(ports: number[] | null | undefined, ranges: PortInterval[] | undefined, separator = ','): string {
  return ranges?.length ? ranges.map(({ first, last }) => first === last ? String(first) : `${first}-${last}`).join(separator)
    : ports?.join(separator) ?? ''
}
export function profilePorts(profile: Pick<Profile, 'protocol' | 'ports' | 'udpPorts' | 'portRanges' | 'udpPortRanges'>): string {
  const primary = `${profile.protocol.toUpperCase()} ${portText(profile.ports, profile.portRanges, ', ')}`
  const udp = portText(profile.udpPorts, profile.udpPortRanges, ', ')
  return udp ? `${primary} / UDP ${udp}` : primary
}
export interface StrategyRecord {
  name: string
  raw: string
  definition: { id: string; revision: string; assets?: string[]; profiles: Profile[]; managedLua?: LuaReference[] }
  listRefs?: ListRefs
  origin?: PresetOrigin
}
export interface StrategyReference {
  outboundRef: string
  strategyRef: string
  enabled: boolean
  state: 'off' | 'applied' | 'pending' | 'blocked'
  desiredRevision?: string
  appliedRevision?: string
  queue?: number
  nativeMatches?: { profiles?: string[] }
}
export interface CatalogView {
  catalog: { records: Record<string, StrategyRecord>; lists?: Record<string, NamedList>; blobAssets?: Record<string, string> }
  revision: string
  ready: boolean
  references: StrategyReference[]
  templates?: PresetTemplate[]
  nativeOnly?: string[]
  engineVersion?: string
  engineCommit?: string
  storageUsed?: number
  luaSourceLimit?: number
  preferenceKey?: string
}
export interface PresetTemplate {
  id: string; name: string; description: string; category: string; compatibility: 'adapted' | 'preview-only'; profiles: number
  origin: string; version: string; commit: string; sourcePath: string; license: string
}
export type PresetFormat = 'template' | 'native' | 'json' | 'zip'
export type PresetInput = { format: PresetFormat; name?: string; templateId?: string; content?: string; dependencyIds?: { path: string; id: string }[] }
export interface PresetPreview {
  source?: string
  origin?: PresetOrigin
  profiles?: { name?: string; line: number; options?: string[] }[]
  issues?: { option?: string; code: string; message: string; span?: { line: number; column: number; end: number } }[]
  dependencies?: { source: string; id: string; kind: string; state: 'new' | 'reuse' | 'conflict' | 'missing' | 'needs-approval'; canRename?: boolean }[]
  record?: StrategyRecord
  converted?: boolean
}
export const presetBytes = 256 * 1024
export const nativeReviewBytes = 64 * 1024
export type ExportFormat = 'json' | 'zip' | 'native'
export type Draft = { name: string; listRefs?: ListRefs; managedLua?: LuaReference[] } & ({ raw: string } | { profiles: Profile[] })

interface Envelope<T> { success: boolean; msg: string; obj: T }
const base = 'api/components/zapret'
function unwrap<T>(response: { data: Envelope<T> }): T {
  if (!response.data.success) throw new Error(response.data.msg || 'Zapret request failed')
  return response.data.obj
}
async function post<T>(path: string, body: object): Promise<T> {
  return unwrap(await api.post<Envelope<T>>(`${base}/${path}`, body, { headers: { 'Content-Type': 'application/json' } }))
}
export async function loadCatalog(): Promise<CatalogView> {
  return unwrap(await api.get<Envelope<CatalogView>>(`${base}/strategies`))
}
export const saveCatalog = (change: object) => post<CatalogView>('strategies', change)
export async function loadLuaDrafts(): Promise<LuaDraftView> {
  return unwrap(await api.get<Envelope<LuaDraftView>>(`${base}/strategies/lua-drafts`))
}
export function saveLuaDraft(action: 'create' | 'edit' | 'import' | 'delete', id: string, expectedRevision: string, draft?: LuaDraft) {
  return post<LuaDraftView>('strategies/lua-drafts', {
    action, id, expectedRevision,
    ...(draft ? { draft: { origin: draft.origin, sourceRef: draft.sourceRef, source: encodePresetBytes(new TextEncoder().encode(draft.source)) } } : {}),
  })
}
export const preview = (draft: Draft, expectedRevision: string) => post<StrategyRecord & { visualEditable?: boolean }>('strategies/preview', { ...draft, expectedRevision })
export const reviewPreset = (preset: PresetInput, expectedRevision: string) => post<PresetPreview>('strategies/import/preview', { preset, expectedRevision })
export const savePreset = (preset: PresetInput, expectedRevision: string) => post<CatalogView>('strategies/import', { preset, expectedRevision })
export const diagnose = (ref: StrategyReference, revision: string) => post<{ OK: boolean; Delay: number; Error: string }>('diagnose', {
  outboundRef: ref.outboundRef, strategyRef: ref.strategyRef, expectedRevision: revision,
})

export async function exportRecord(id: string, expectedRevision: string, format: ExportFormat): Promise<Blob> {
  const path = `${base}/strategies/${encodeURIComponent(id)}/export`
  const params = { expectedRevision, format }
  if (format === 'json') {
    const data = unwrap(await api.get<Envelope<unknown>>(path, { params }))
    return new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
  }
  const { data } = await api.get<Blob>(path, { params, responseType: 'blob' })
  if (data.type.includes('application/json')) unwrap({ data: JSON.parse(await data.text()) as Envelope<unknown> })
  return data
}

export function encodePresetBytes(data: Uint8Array): string {
  let text = ''
  for (let i = 0; i < data.length; i += 8192) text += String.fromCharCode(...data.subarray(i, i + 8192))
  return btoa(text)
}

export function catalogError(err: unknown, fallback: string): string {
  const detail = (err as { response?: { data?: { msg?: unknown } } })?.response?.data?.msg
  return typeof detail === 'string' ? detail : err instanceof Error ? err.message : fallback
}

export function draftToSave(name: string, raw: string, profiles: Profile[], visualChanged: boolean, listRefs: ListRefs, managedLua: LuaReference[] = []): Draft {
  const source = visualChanged ? { profiles } : { raw }
  return { name, ...source, ...(Object.keys(listRefs).length ? { listRefs } : {}), ...(managedLua.length ? { managedLua } : {}) }
}
