import api from '@solovey/component-ui/api'

export interface Profile {
  id: string
  protocol: 'tcp'
  ports: number[]
  payload?: 'tls_client_hello' | 'http_req'
  splitAt?: number
}
export interface StrategyRecord {
  name: string
  raw: string
  definition: { id: string; revision: string; assets?: string[]; profiles: Profile[] }
}
export interface StrategyReference {
  outboundRef: string
  strategyRef: string
  enabled: boolean
  state: 'off' | 'applied' | 'pending' | 'blocked'
  desiredRevision?: string
  appliedRevision?: string
}
export interface CatalogView {
  catalog: { records: Record<string, StrategyRecord> }
  revision: string
  ready: boolean
  references: StrategyReference[]
  transferSchema: string
  engineCommit: string
  engineLicense: string
}
export type Draft = { name: string } & ({ raw: string } | { profiles: Profile[] })

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
export const preview = (draft: Draft) => post<StrategyRecord>('strategies/preview', draft)
export const diagnose = (ref: StrategyReference, revision: string) => post<{ OK: boolean; Delay: number; Error: string }>('diagnose', {
  outboundRef: ref.outboundRef, strategyRef: ref.strategyRef, expectedRevision: revision,
})

export function exportRecord(catalog: CatalogView, record: StrategyRecord) {
  return { schema: catalog.transferSchema, engineCommit: catalog.engineCommit, license: catalog.engineLicense,
    assets: record.definition.assets ?? [], strategy: record }
}

export function draftToSave(name: string, raw: string, profiles: Profile[], visualChanged: boolean): Draft {
  return visualChanged ? { name, profiles } : { name, raw }
}
