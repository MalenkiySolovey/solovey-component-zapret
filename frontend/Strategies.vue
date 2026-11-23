<template>
  <v-container fluid class="zapret-catalog">
    <header class="d-flex flex-wrap align-center justify-space-between ga-3 mb-4">
      <h2>{{ t('zapretCatalog.title') }}</h2>
      <div class="d-flex flex-wrap ga-2">
        <v-btn prepend-icon="mdi-refresh" variant="text" :disabled="busy" @click="refresh">{{ t('zapretCatalog.refresh') }}</v-btn>
        <v-btn prepend-icon="mdi-upload" variant="tonal" :disabled="busy || !view" @click="importField?.click()">{{ t('zapretCatalog.import') }}</v-btn>
        <v-btn prepend-icon="mdi-plus" color="primary" :disabled="busy || !view" @click="openDraft()">{{ t('zapretCatalog.create') }}</v-btn>
        <input ref="importField" type="file" accept=".json,application/json" hidden @change="importRecord" />
      </div>
    </header>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <v-alert v-if="view && !view.ready" type="warning" variant="tonal" class="mb-3">{{ t('zapretCatalog.unavailable') }}</v-alert>
    <v-progress-linear v-if="busy" indeterminate />
    <v-list v-if="view" lines="two" class="border rounded-lg mb-4">
      <v-list-item v-for="record in records" :key="record.definition.id" :title="record.name" :subtitle="record.definition.id">
        <template #append>
          <v-btn icon="mdi-pencil-outline" variant="text" :title="t('zapretCatalog.edit')" :aria-label="t('zapretCatalog.edit')" :disabled="busy" @click="openDraft(record)" />
          <v-btn icon="mdi-content-copy" variant="text" :title="t('zapretCatalog.clone')" :aria-label="t('zapretCatalog.clone')" :disabled="busy" @click="openDraft(record, true)" />
          <v-btn icon="mdi-download" variant="text" :title="t('zapretCatalog.export')" :aria-label="t('zapretCatalog.export')" :disabled="busy" @click="download(record)" />
          <v-btn icon="mdi-delete-outline" variant="text" :title="t('zapretCatalog.delete')" :aria-label="t('zapretCatalog.delete')" :disabled="busy || refsFor(record.definition.id).length > 0" @click="deleteRecord = record" />
        </template>
        <div v-if="refsFor(record.definition.id).length" class="text-caption mt-1">{{ t('zapretCatalog.references') }}: {{ refsFor(record.definition.id).map(ref => ref.outboundRef).join(', ') }}</div>
      </v-list-item>
      <v-list-item v-if="records.length === 0" :title="t('zapretCatalog.empty')" />
    </v-list>
    <section v-if="view" class="mb-4" aria-labelledby="zapret-native-list-title">
      <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-2">
        <h3 id="zapret-native-list-title">{{ t('zapretCatalog.sharedLists') }}</h3>
        <v-btn prepend-icon="mdi-plus" variant="tonal" :disabled="busy || namedLists.length >= 16" @click="openList()">{{ t('zapretCatalog.newList') }}</v-btn>
      </div>
      <v-list density="compact" class="border rounded-lg">
        <v-list-item v-for="[id, list] in namedLists" :key="id" :title="id" :subtitle="`${t(`zapretCatalog.${list.kind}Kind`)} · ${list.entries.length}`">
          <template #append>
            <v-btn icon="mdi-pencil-outline" variant="text" :aria-label="t('zapretCatalog.editList')" :title="t('zapretCatalog.editList')" :disabled="busy" @click="openList(id, list)" />
            <v-btn icon="mdi-delete-outline" variant="text" :aria-label="t('zapretCatalog.deleteList')" :title="t('zapretCatalog.deleteList')" :disabled="busy || listConsumers(id).length > 0" @click="deletingAsset = { id, blob: false }" />
          </template>
          <div v-if="listConsumers(id).length" class="text-caption mt-1">{{ t('zapretCatalog.references') }}: {{ listConsumers(id).map(record => record.name).join(', ') }}</div>
        </v-list-item>
        <v-list-item v-if="!namedLists.length" :title="t('zapretCatalog.noLists')" />
      </v-list>
    </section>
    <section v-if="view" class="mb-4" aria-labelledby="zapret-native-blob-title">
      <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-2">
        <h3 id="zapret-native-blob-title">{{ t('zapretCatalog.sharedBlobs') }}</h3>
        <v-btn prepend-icon="mdi-plus" variant="tonal" :disabled="busy || namedBlobs.length >= 8" @click="openList('', undefined, '')">{{ t('zapretCatalog.newBlob') }}</v-btn>
      </div>
      <v-list density="compact" class="border rounded-lg">
        <v-list-item v-for="[id, data] in namedBlobs" :key="id" :title="id" :subtitle="t('zapretCatalog.blobSize', { count: data.length / 2 })">
          <template #append>
            <v-btn icon="mdi-pencil-outline" variant="text" :aria-label="t('zapretCatalog.editBlob')" :title="t('zapretCatalog.editBlob')" :disabled="busy" @click="openList(id, undefined, data)" />
            <v-btn icon="mdi-delete-outline" variant="text" :aria-label="t('zapretCatalog.deleteBlob')" :title="t('zapretCatalog.deleteBlob')" :disabled="busy || blobConsumers(id).length > 0" @click="deletingAsset = { id, blob: true }" />
          </template>
          <div v-if="blobConsumers(id).length" class="text-caption mt-1">{{ t('zapretCatalog.references') }}: {{ blobConsumers(id).map(record => record.name).join(', ') }}</div>
        </v-list-item>
        <v-list-item v-if="!namedBlobs.length" :title="t('zapretCatalog.noBlobs')" />
      </v-list>
    </section>
    <section v-if="view?.references.length" aria-labelledby="zapret-binding-status">
      <h3 id="zapret-binding-status" class="mb-2">{{ t('zapretCatalog.bindings') }}</h3>
      <v-table density="compact">
        <thead><tr><th>{{ t('zapretCatalog.direct') }}</th><th>{{ t('zapretCatalog.strategy') }}</th><th>{{ t('zapretCatalog.status') }}</th><th>NFQUEUE</th><th>{{ t('zapretCatalog.diagnostic') }}</th></tr></thead>
        <tbody>
          <tr v-for="ref in view.references" :key="ref.outboundRef">
            <td>{{ ref.outboundRef }}</td>
            <td>{{ view.catalog.records[ref.strategyRef]?.name ?? ref.strategyRef }} <code v-if="ref.desiredRevision" :title="ref.desiredRevision">{{ ref.desiredRevision.slice(0, 8) }}</code></td>
            <td>
              {{ t(`zapretCatalog.states.${ref.state}`) }} <code v-if="ref.appliedRevision" :title="ref.appliedRevision">{{ ref.appliedRevision.slice(0, 8) }}</code>
              <div v-if="ref.state === 'applied' && ref.nativeMatches" class="text-caption text-break">
                <template v-if="ref.nativeMatches.profiles?.length">
                  {{ t('zapretCatalog.observedProfiles') }}: <code>{{ ref.nativeMatches.profiles.join(', ') }}</code>
                </template>
                <template v-else>{{ t('zapretCatalog.noObservedProfiles') }}</template>
              </div>
            </td>
            <td><code v-if="ref.queue">{{ ref.queue }}</code><span v-else>-</span></td>
            <td><v-btn icon="mdi-pulse" variant="text" :title="t('zapretCatalog.diagnostic')" :aria-label="t('zapretCatalog.diagnostic')" :disabled="busy || ref.state !== 'applied'" @click="runDiagnostic(ref)" /></td>
          </tr>
        </tbody>
      </v-table>
      <v-alert v-if="diagnostic" :type="diagnostic.ok ? 'success' : 'warning'" variant="tonal" class="mt-3" role="status">{{ diagnostic.text }}</v-alert>
    </section>
    <v-dialog v-model="editing" max-width="760" persistent>
      <v-card :title="activeID ? t('zapretCatalog.edit') : t('zapretCatalog.create')">
        <v-card-text>
          <v-alert v-if="dialogError" type="error" variant="tonal" class="mb-3">{{ dialogError }}</v-alert>
          <v-alert v-if="affected.length" type="warning" variant="tonal" class="mb-3">{{ t('zapretCatalog.editAffects', { refs: affected.map(ref => ref.outboundRef).join(', ') }) }}</v-alert>
          <v-text-field v-model="name" :label="t('zapretCatalog.name')" maxlength="120" />
          <div v-if="!activeID" class="d-flex flex-wrap ga-2 mb-3">
            <v-btn size="small" variant="tonal" @click="preset('tls_client_hello', '443,853')">{{ t('zapretCatalog.tlsPreset') }}</v-btn>
            <v-btn size="small" variant="tonal" @click="preset('http_req', '80')">{{ t('zapretCatalog.httpPreset') }}</v-btn>
            <v-btn size="small" variant="tonal" @click="preset('quic_initial', '443,853')">{{ t('zapretCatalog.quicPreset') }}</v-btn>
          </div>
          <v-btn-toggle :model-value="mode" mandatory density="compact" class="mb-3" @update:model-value="switchMode">
            <v-btn value="visual" :disabled="busy">{{ t('zapretCatalog.visual') }}</v-btn>
            <v-btn value="raw" :disabled="busy">{{ t('zapretCatalog.raw') }}</v-btn>
          </v-btn-toggle>
          <template v-if="mode === 'raw'">
            <v-textarea v-model="raw" class="native-source" :label="t('zapretCatalog.raw')" spellcheck="false" maxlength="8192" rows="8" />
            <dl v-if="Object.keys(rawRefs).length" class="text-caption">
              <template v-for="(refs, id) in rawRefs" :key="id">
                <dt class="font-weight-medium">{{ id }}</dt>
                <dd v-for="(list, field) in refs" :key="field">{{ t(`zapretCatalog.${field}`) }}: {{ list }}</dd>
              </template>
            </dl>
          </template>
          <template v-else>
            <div v-for="(row, index) in rows" :key="row.key" class="profile-row py-3 border-b">
              <div class="d-flex justify-space-between align-center flex-wrap ga-2">
                <strong>{{ t('zapretCatalog.profile', { index: index + 1 }) }}</strong>
                <div>
                  <v-btn icon="mdi-chevron-up" size="small" variant="text" :disabled="index === 0" :title="t('zapretCatalog.moveUp')" :aria-label="t('zapretCatalog.moveUp')" @click="move(index, -1)" />
                  <v-btn icon="mdi-chevron-down" size="small" variant="text" :disabled="index === rows.length - 1" :title="t('zapretCatalog.moveDown')" :aria-label="t('zapretCatalog.moveDown')" @click="move(index, 1)" />
                  <v-btn icon="mdi-close" size="small" variant="text" :disabled="rows.length <= 1" :title="t('zapretCatalog.removeProfile')" :aria-label="t('zapretCatalog.removeProfile')" @click="remove(index)" />
                </div>
              </div>
              <div class="profile-fields">
                <v-text-field v-model="row.id" :label="t('zapretCatalog.profileID')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-select :model-value="row.protocol" :items="['tcp', 'udp']" :label="t('zapretCatalog.protocol')" hide-details density="compact" @update:model-value="changeProtocol(row, $event)" />
                <v-select v-model="row.family" :items="families" :label="t('zapretCatalog.family')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-text-field v-model="row.ports" :label="t('zapretCatalog.ports')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-select v-model="row.payload" :items="payloads(row.protocol)" :label="t('zapretCatalog.payload')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-text-field v-if="row.payload === 'quic_initial'" v-model="row.quicRepeats" type="number" min="1" max="4" :label="t('zapretCatalog.quicRepeats')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-text-field v-else-if="row.payload" v-model="row.splitAt" type="number" min="1" max="128" :label="t('zapretCatalog.split')" hide-details density="compact" @update:model-value="visualChanged = true" />
              </div>
              <v-select v-if="row.payload === 'quic_initial' && blobChoices.length > 1" v-model="row.quicBlobRef" :items="blobChoices"
                :label="t('zapretCatalog.quicBlob')" class="mt-2" hide-details density="compact" @update:model-value="visualChanged = true" />
              <details class="mt-2" :open="listFields.some(field => row.lists[field] !== '' || row.refs[field]) || undefined">
                <summary>{{ t('zapretCatalog.nativeLists') }}</summary>
                <div class="list-fields mt-2">
                  <div v-for="field in listFields" :key="field">
                    <v-select v-if="listChoices(field).length" :model-value="row.refs[field] ?? null" :items="listChoices(field)" clearable
                      :label="t('zapretCatalog.useList', { field: t(`zapretCatalog.${field}`) })" :disabled="!!row.lists[field].trim()"
                      density="compact" hide-details @update:model-value="setListRef(row, field, $event)" />
                    <v-textarea v-if="!row.refs[field]" v-model="row.lists[field]" :label="t(`zapretCatalog.${field}`)" rows="2"
                      maxlength="2048" density="compact" hide-details spellcheck="false" @update:model-value="visualChanged = true" />
                  </div>
                </div>
              </details>
            </div>
            <v-btn variant="text" prepend-icon="mdi-plus" :disabled="rows.length >= 16" @click="addProfile">{{ t('zapretCatalog.addProfile') }}</v-btn>
          </template>
          <div class="text-caption text-medium-emphasis">{{ t('zapretCatalog.assetScope') }}</div>
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn :disabled="busy" @click="editing = false">{{ t('zapretCatalog.cancel') }}</v-btn>
          <v-btn :loading="busy" :disabled="!name.trim()" color="primary" variant="tonal" prepend-icon="mdi-content-save-outline" @click="save">{{ t('zapretCatalog.save') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog :model-value="deleteRecord !== undefined" max-width="440" @update:model-value="deleteRecord = undefined">
      <v-card :title="t('zapretCatalog.delete')" :text="t('zapretCatalog.deleteConfirm', { name: deleteRecord?.name ?? '' })">
        <v-card-actions><v-spacer /><v-btn :disabled="busy" @click="deleteRecord = undefined">{{ t('zapretCatalog.cancel') }}</v-btn>
          <v-btn color="error" :loading="busy" @click="removeRecord">{{ t('zapretCatalog.delete') }}</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog v-model="listEditing" max-width="520" persistent>
      <v-card :title="t(editingBlob ? (listActiveID ? 'zapretCatalog.editBlob' : 'zapretCatalog.newBlob') : (listActiveID ? 'zapretCatalog.editList' : 'zapretCatalog.newList'))">
        <v-card-text>
          <v-alert v-if="dialogError" type="error" variant="tonal" class="mb-3">{{ dialogError }}</v-alert>
          <v-alert v-if="listAffected.length" type="warning" variant="tonal" class="mb-3">{{ t('zapretCatalog.editAffects', { refs: listAffected.join(', ') }) }}</v-alert>
          <v-text-field v-model="listID" :label="t(editingBlob ? 'zapretCatalog.blobID' : 'zapretCatalog.listID')" :readonly="!!listActiveID" maxlength="64" />
          <v-select v-if="!editingBlob" v-model="listKind" :items="listKinds" :label="t('zapretCatalog.listKind')" :readonly="!!listActiveID" />
          <v-textarea v-model="listEntries" :label="t(editingBlob ? 'zapretCatalog.blobHex' : 'zapretCatalog.listEntries')" rows="5"
            :maxlength="editingBlob ? 2400 : 2048" spellcheck="false" />
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn :disabled="busy" @click="listEditing = false">{{ t('zapretCatalog.cancel') }}</v-btn>
          <v-btn :loading="busy" :disabled="!listID.trim() || !listEntries.trim()" color="primary" variant="tonal" prepend-icon="mdi-content-save-outline" @click="saveList">{{ t('zapretCatalog.save') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog :model-value="!!deletingAsset" max-width="440" @update:model-value="deletingAsset = undefined">
      <v-card :title="deleteAssetLabel" :text="t('zapretCatalog.deleteConfirm', { name: deletingAsset?.id })">
        <v-card-actions><v-spacer /><v-btn :disabled="busy" @click="deletingAsset = undefined">{{ t('zapretCatalog.cancel') }}</v-btn>
          <v-btn color="error" :loading="busy" @click="removeList">{{ deleteAssetLabel }}</v-btn></v-card-actions>
      </v-card>
    </v-dialog>
  </v-container>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { diagnose, draftToSave, exportRecord, loadCatalog, preview, saveCatalog, type CatalogView, type ListKind, type ListRefs, type NamedList, type NativeListField, type Profile, type ProfileListRefs, type StrategyRecord, type StrategyReference } from './catalog'

const { t } = useI18n()
type Row = { key: number; id: string; protocol: Profile['protocol']; family: NonNullable<Profile['family']> | ''; ports: string;
  payload: NonNullable<Profile['payload']> | ''; splitAt: string; quicRepeats: string; quicBlobRef: string; lists: Record<NativeListField, string>; refs: ProfileListRefs }
const listFields = ['hostList', 'hostListExclude', 'ipSet', 'ipSetExclude'] as const
const view = ref<CatalogView>()
const busy = ref(false)
const error = ref('')
const dialogError = ref('')
const diagnostic = ref<{ ok: boolean; text: string }>()
const importField = ref<HTMLInputElement>()
const deleteRecord = ref<StrategyRecord>()
const editing = ref(false)
const activeID = ref('')
const editorRevision = ref('')
const listEditing = ref(false)
const listActiveID = ref('')
const listID = ref('')
const listKind = ref<ListKind>('hostList')
const listEntries = ref('')
const listEditorRevision = ref('')
const editingBlob = ref(false)
const deletingAsset = ref<{ id: string; blob: boolean }>()
const deleteAssetLabel = computed(() => t(deletingAsset.value?.blob ? 'zapretCatalog.deleteBlob' : 'zapretCatalog.deleteList'))
const name = ref('')
const raw = ref('')
const rawRefs = ref<ListRefs>({})
const mode = ref<'visual' | 'raw'>('visual')
const rows = ref<Row[]>([])
const visualChanged = ref(false)
let rowKey = 0
const records = computed(() => Object.values(view.value?.catalog.records ?? {}).sort((a, b) => a.name.localeCompare(b.name)))
const namedLists = computed(() => Object.entries(view.value?.catalog.lists ?? {}).sort(([a], [b]) => a.localeCompare(b)))
const namedBlobs = computed(() => Object.entries(view.value?.catalog.blobAssets ?? {}).sort(([a], [b]) => a.localeCompare(b)))
const blobChoices = computed(() => [{ title: t('zapretCatalog.defaultBlob'), value: '' }, ...namedBlobs.value.map(([id]) => ({ title: id, value: id }))])
const listKinds = computed<{ title: string; value: ListKind }[]>(() => [
  { title: t('zapretCatalog.hostListKind'), value: 'hostList' }, { title: t('zapretCatalog.ipSetKind'), value: 'ipSet' },
])
const listChoices = (field: NativeListField) => namedLists.value.filter(([, list]) => list.kind === (field.startsWith('hostList') ? 'hostList' : 'ipSet')).map(([id]) => id)
const listConsumers = (id: string) => records.value.filter(record => Object.values(record.listRefs ?? {}).some(refs => Object.values(refs).includes(id)))
const blobConsumers = (id: string) => records.value.filter(record => record.definition.profiles.some(p => p.quicBlobRef === id))
const families = computed(() => [{ title: t('zapretCatalog.availableFamilies'), value: '' }, { title: 'IPv4', value: 'ipv4' }, { title: 'IPv6', value: 'ipv6' }])
const payloads = (protocol: Profile['protocol']) => [{ title: t('zapretCatalog.passive'), value: '' }, ...(protocol === 'udp'
  ? [{ title: 'QUIC Initial', value: 'quic_initial' }]
  : [{ title: 'TLS ClientHello', value: 'tls_client_hello' }, { title: 'HTTP request', value: 'http_req' }])]
const refsFor = (id: string) => view.value?.references.filter(ref => ref.strategyRef === id) ?? []
const affected = computed(() => refsFor(activeID.value).filter(ref => ref.enabled))
const listAffected = computed(() => listActiveID.value ? (editingBlob.value ? blobConsumers : listConsumers)(listActiveID.value)
  .flatMap(record => refsFor(record.definition.id).filter(ref => ref.enabled).map(ref => ref.outboundRef)) : [])
const errorText = (err: unknown) => {
  const detail = (err as { response?: { data?: { msg?: unknown } } })?.response?.data?.msg
  return typeof detail === 'string' ? detail : err instanceof Error ? err.message : t('zapretCatalog.unavailable')
}
async function run(action: () => Promise<void>, inDialog = false) {
  if (busy.value) return
  busy.value = true
  error.value = dialogError.value = ''
  try { await action() } catch (err) { (inDialog ? dialogError : error).value = errorText(err) }
  finally { busy.value = false }
}
const refresh = () => run(async () => { view.value = await loadCatalog() })
onMounted(refresh)
function listText(lists: Profile['lists'] = {}): Row['lists'] {
  return { hostList: lists.hostList?.join('\n') ?? '', hostListExclude: lists.hostListExclude?.join('\n') ?? '',
    ipSet: lists.ipSet?.join('\n') ?? '', ipSetExclude: lists.ipSetExclude?.join('\n') ?? '' }
}
function withRows(record?: StrategyRecord) {
  rawRefs.value = { ...record?.listRefs }
  rows.value = (record?.definition.profiles ?? []).map(p => {
    const refs = { ...rawRefs.value[p.id] }
    const lists = listText(p.lists)
    for (const field of listFields) if (refs[field]) lists[field] = ''
    return { key: ++rowKey, id: p.id, protocol: p.protocol, family: p.family ?? '', ports: p.ports.join(','),
      payload: p.payload ?? '', splitAt: String(p.splitAt ?? 9), quicRepeats: String(p.quicRepeats ?? 1), quicBlobRef: p.quicBlobRef ?? '', lists, refs }
  })
}
function setListRef(row: Row, field: NativeListField, value: string | null) {
  if (value) row.refs[field] = value
  else delete row.refs[field]
  visualChanged.value = true
}
function changeProtocol(row: Row, protocol: Profile['protocol']) {
  if (row.protocol === protocol) return
  row.protocol = protocol
  row.payload = ''
  row.quicBlobRef = ''
  visualChanged.value = true
}
function addProfile() {
  let index = 1
  while (rows.value.some(row => row.id === `profile-${index}`)) index++
  rows.value.push({ key: ++rowKey, id: `profile-${index}`, protocol: 'tcp', family: '', ports: '443', payload: '', splitAt: '9', quicRepeats: '1', quicBlobRef: '', lists: listText(), refs: {} })
  visualChanged.value = true
}
function remove(index: number) { rows.value.splice(index, 1); visualChanged.value = true }
function move(index: number, offset: number) {
  const [row] = rows.value.splice(index, 1)
  rows.value.splice(index + offset, 0, row)
  visualChanged.value = true
}
function preset(payload: NonNullable<Profile['payload']>, ports: string) {
  rows.value = [{ key: ++rowKey, id: 'profile-1', protocol: payload === 'quic_initial' ? 'udp' : 'tcp', family: '', ports, payload,
    splitAt: '9', quicRepeats: '1', quicBlobRef: '', lists: listText(), refs: {} }]
  mode.value = 'visual'
  visualChanged.value = true
}
function openDraft(record?: StrategyRecord, clone = false) {
  activeID.value = clone ? '' : record?.definition.id ?? ''
  editorRevision.value = view.value?.revision ?? ''
  name.value = clone ? t('zapretCatalog.copyName', { name: record?.name }) : record?.name ?? ''
  raw.value = record?.raw ?? ''
  mode.value = 'visual'
  dialogError.value = ''
  withRows(record)
  visualChanged.value = false
  if (!record) addProfile()
  editing.value = true
}
function draft() {
  const profiles = rows.value.map<Profile>(row => {
    const lists: NonNullable<Profile['lists']> = {}
    for (const field of listFields) {
      const entries = row.lists[field].split(/[\s,]+/).filter(Boolean)
      if (entries.length) lists[field] = entries
    }
    return { id: row.id.trim(), protocol: row.protocol, ports: row.ports.split(',').map(port => Number(port.trim())),
      ...(row.family ? { family: row.family } : {}), ...(row.payload === 'quic_initial' ? { payload: row.payload, quicRepeats: Number(row.quicRepeats),
        ...(row.quicBlobRef ? { quicBlobRef: row.quicBlobRef } : {}) }
        : row.payload ? { payload: row.payload, splitAt: Number(row.splitAt) } : {}), ...(Object.keys(lists).length ? { lists } : {}) }
  })
  const refs = visualChanged.value ? Object.fromEntries(rows.value.filter(row => Object.keys(row.refs).length).map(row => [row.id.trim(), row.refs])) : rawRefs.value
  return draftToSave(name.value.trim(), raw.value, profiles, visualChanged.value, refs)
}
async function switchMode(next: 'visual' | 'raw') {
  if (next === mode.value) return
  await run(async () => {
    if (next === 'visual' || visualChanged.value) {
      const record = await preview({ ...draft(), name: name.value.trim() || 'Preview' }, editorRevision.value)
      raw.value = record.raw
      withRows(record)
    }
    visualChanged.value = false
    mode.value = next
  }, true)
}
const save = () => run(async () => {
  const id = activeID.value
  view.value = await saveCatalog({ action: id ? 'edit' : 'create', ...(id ? { id } : {}), expectedRevision: editorRevision.value, draft: draft() })
  editing.value = false
}, true)
const removeRecord = () => run(async () => {
  if (!deleteRecord.value || !view.value) return
  view.value = await saveCatalog({ action: 'delete', id: deleteRecord.value.definition.id, expectedRevision: view.value.revision })
  deleteRecord.value = undefined
})
function openList(id = '', list?: NamedList, blob?: string) {
  editingBlob.value = blob !== undefined
  listActiveID.value = listID.value = id
  listEditorRevision.value = view.value?.revision ?? ''
  listKind.value = list?.kind ?? 'hostList'
  listEntries.value = blob ?? list?.entries.join('\n') ?? ''
  dialogError.value = ''
  listEditing.value = true
}
const saveList = () => run(async () => {
  const action = `${listActiveID.value ? 'edit' : 'create'}-${editingBlob.value ? 'blob' : 'list'}`
  const data = editingBlob.value ? { blob: listEntries.value.trim() } : { list: { kind: listKind.value, entries: listEntries.value.split(/[\s,]+/).filter(Boolean) } }
  view.value = await saveCatalog({ action, id: listID.value.trim(), expectedRevision: listEditorRevision.value, ...data })
  listEditing.value = false
}, true)
const removeList = () => run(async () => {
  const asset = deletingAsset.value
  if (!asset || !view.value) return
  view.value = await saveCatalog({ action: asset.blob ? 'delete-blob' : 'delete-list', id: asset.id, expectedRevision: view.value.revision })
  deletingAsset.value = undefined
})
const runDiagnostic = (ref: StrategyReference) => run(async () => {
  if (!view.value) return
  diagnostic.value = undefined
  const result = await diagnose(ref, view.value.revision)
  diagnostic.value = { ok: result.OK, text: result.OK ? t('zapretCatalog.testOK', { direct: ref.outboundRef, delay: result.Delay }) : `${ref.outboundRef}: ${result.Error}` }
})
const download = (record: StrategyRecord) => run(async () => {
  if (!view.value) return
  const data = await exportRecord(record.definition.id, view.value.revision)
  const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }))
  const link = document.createElement('a')
  link.href = url
  link.download = `${record.definition.id}.json`
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
})
function importRecord(event: Event) {
  const field = event.target as HTMLInputElement
  const file = field.files?.[0]
  field.value = ''
  if (!file || !view.value) return
  const revision = view.value.revision
  void run(async () => {
    if (file.size > 32 * 1024) throw new Error(t('zapretCatalog.importInvalid'))
    let transfer: unknown
    try { transfer = JSON.parse(await file.text()) } catch { throw new Error(t('zapretCatalog.importInvalid')) }
    view.value = await saveCatalog({ action: 'import', expectedRevision: revision, transfer })
  })
}
</script>

<style scoped>
.zapret-catalog { max-width: 1120px; }
.profile-fields { display: grid; gap: 8px; grid-template-columns: repeat(auto-fit, minmax(min(100%, 140px), 1fr)); }
.list-fields { display: grid; gap: 8px; grid-template-columns: repeat(auto-fit, minmax(min(100%, 276px), 1fr)); }
.native-source :deep(textarea) {font-family: ui-monospace, monospace;}
</style>
