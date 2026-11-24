import api from '@solovey/component-ui/api'

export type NativeListField = 'hostList' | 'hostListExclude' | 'ipSet' | 'ipSetExclude'
export type ListKind = 'hostList' | 'ipSet'
export type NamedList = { kind: ListKind; entries: string[] }
export type ProfileListRefs = Partial<Record<NativeListField, string>>
export type ListRefs = Record<string, ProfileListRefs>
export type HTTPAction = { name: 'http_domcase' | 'http_hostcase'; spell?: string }

export interface Profile {
  id: string
  protocol: 'tcp' | 'udp'
  family?: 'ipv4' | 'ipv6'
  ports: number[]
  payload?: 'tls_client_hello' | 'http_req' | 'quic_initial'
  splitAt?: number
  circular?: { failures: number; splits: number[] }
  httpActions?: HTTPAction[]
  quicRepeats?: number
  quicBlobRef?: string
  lists?: Partial<Record<NativeListField, string[]>>
}
export interface StrategyRecord {
  name: string
  raw: string
  definition: { id: string; revision: string; assets?: string[]; profiles: Profile[] }
  listRefs?: ListRefs
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
}
export type Draft = { name: string; listRefs?: ListRefs } & ({ raw: string } | { profiles: Profile[] })

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
export const preview = (draft: Draft, expectedRevision: string) => post<StrategyRecord>('strategies/preview', { ...draft, expectedRevision })
export const diagnose = (ref: StrategyReference, revision: string) => post<{ OK: boolean; Delay: number; Error: string }>('diagnose', {
  outboundRef: ref.outboundRef, strategyRef: ref.strategyRef, expectedRevision: revision,
})

export async function exportRecord(id: string, expectedRevision: string): Promise<unknown> {
  return unwrap(await api.get<Envelope<unknown>>(`${base}/strategies/${encodeURIComponent(id)}/export`, { params: { expectedRevision } }))
}

export function draftToSave(name: string, raw: string, profiles: Profile[], visualChanged: boolean, listRefs: ListRefs): Draft {
  const source = visualChanged ? { profiles } : { raw }
  return { name, ...source, ...(Object.keys(listRefs).length ? { listRefs } : {}) }
}
