<template>
  <section aria-labelledby="zapret-lua-title">
    <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-2">
      <h2 id="zapret-lua-title" class="text-subtitle-1">{{ t('zapretCatalog.lua.title') }}</h2>
      <div v-if="snapshot" class="d-flex flex-wrap ga-1">
        <v-btn prepend-icon="mdi-upload" variant="text" :disabled="disabled || busy" @click="openDraft('import')">
          {{ t('zapretCatalog.lua.import') }}
        </v-btn>
        <v-btn prepend-icon="mdi-plus" variant="tonal" :disabled="disabled || busy" @click="openDraft('create')">
          {{ t('zapretCatalog.lua.create') }}
        </v-btn>
      </div>
    </div>
    <p class="text-body-2 text-medium-emphasis mb-2">{{ t('zapretCatalog.lua.pinned') }}</p>
    <v-alert type="info" variant="tonal" density="compact" class="mb-3">{{ t('zapretCatalog.lua.inactive') }}</v-alert>
    <v-alert v-if="accessMessage" type="warning" variant="tonal" density="compact" class="mb-3">{{ accessMessage }}</v-alert>
    <v-progress-linear v-if="busy && !snapshot" indeterminate />
    <v-list v-if="snapshot" density="compact" class="border rounded-lg">
      <v-list-item v-for="[id, item] in filteredDrafts" :key="id" :title="id" :subtitle="t(`zapretCatalog.lua.origins.${item.origin}`)">
        <p v-if="item.sourceRef" class="text-caption text-break mt-1">{{ item.sourceRef }}</p>
        <template #append>
          <div class="d-flex flex-wrap justify-end lua-actions">
            <v-btn icon="mdi-pencil-outline" size="small" variant="text" :aria-label="t('zapretCatalog.lua.edit')" :title="t('zapretCatalog.lua.edit')"
              :disabled="disabled || busy" @click="openDraft('edit', id, item)" />
            <v-btn icon="mdi-download" size="small" variant="text" :aria-label="t('zapretCatalog.lua.export')" :title="t('zapretCatalog.lua.export')"
              :disabled="disabled || busy" @click="downloadSource(id, item.source)" />
            <v-btn icon="mdi-delete-outline" size="small" variant="text" :aria-label="t('zapretCatalog.lua.delete')" :title="t('zapretCatalog.lua.delete')"
              :disabled="disabled || busy" @click="openDelete(id)" />
          </div>
        </template>
      </v-list-item>
      <v-list-item v-if="!filteredDrafts.length" :title="t(drafts.length ? 'zapretCatalog.noMatches' : 'zapretCatalog.lua.empty')" />
    </v-list>
    <v-dialog :model-value="!!editor" max-width="700" persistent scrollable>
      <v-card v-if="editor" :title="t(`zapretCatalog.lua.${editor.mode}`)">
        <v-card-text>
          <v-alert v-if="dialogError" type="error" variant="tonal" class="mb-3">{{ dialogError }}</v-alert>
          <v-chip size="small" variant="tonal" class="mb-3">{{ t('zapretCatalog.lua.notExecutable') }}</v-chip>
          <template v-if="editor.mode === 'import'">
            <input ref="fileField" type="file" accept=".lua" class="d-none" :aria-label="t('zapretCatalog.lua.import')" @change="readFile" />
            <div class="d-flex flex-wrap align-center ga-2 mb-3">
              <v-btn prepend-icon="mdi-upload" variant="tonal" :disabled="busy" @click="fileField?.click()">
                {{ t('zapretCatalog.chooseFile') }}
              </v-btn>
              <span v-if="editor.file" class="text-caption text-break">{{ editor.file }}</span>
            </div>
          </template>
          <v-text-field v-model="editor.id" :label="t('zapretCatalog.lua.id')" :readonly="editor.mode === 'edit'" :disabled="busy" maxlength="64" />
          <v-select v-model="editor.draft.origin" :items="originChoices" :label="t('zapretCatalog.lua.origin')" :disabled="busy" />
          <v-text-field v-model="editor.draft.sourceRef" :label="t('zapretCatalog.lua.sourceRef')" :disabled="busy" maxlength="240" />
          <v-alert v-if="editor.draft.origin !== 'administrator'" type="warning" variant="tonal" density="compact" class="mb-3">
            {{ t('zapretCatalog.lua.external') }}
          </v-alert>
          <v-textarea :model-value="editor.draft.source" :label="t('zapretCatalog.lua.source')" :disabled="busy"
            class="lua-source" rows="8" spellcheck="false" @update:model-value="editSource" />
        </v-card-text>
        <v-card-actions class="flex-wrap justify-end ga-1">
          <v-btn v-if="editor.draft.source" variant="text" :disabled="busy" prepend-icon="mdi-download"
            @click="downloadSource(editor.id, editor.draft.source)">{{ t('zapretCatalog.lua.export') }}</v-btn>
          <v-btn :disabled="busy" @click="editor = undefined">{{ t('zapretCatalog.cancel') }}</v-btn>
          <v-btn :loading="busy" :disabled="!canSave" color="primary" variant="tonal" prepend-icon="mdi-content-save-outline" @click="save">
            {{ t('zapretCatalog.lua.save') }}
          </v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
    <v-dialog :model-value="!!deleting" max-width="440" persistent>
      <v-card v-if="deleting" :title="t('zapretCatalog.lua.delete')">
        <v-card-text>
          <v-alert v-if="dialogError" type="error" variant="tonal" class="mb-3">{{ dialogError }}</v-alert>
          {{ t('zapretCatalog.deleteConfirm', { name: deleting.id }) }}
        </v-card-text>
        <v-card-actions>
          <v-spacer />
          <v-btn :disabled="busy" @click="deleting = undefined">{{ t('zapretCatalog.cancel') }}</v-btn>
          <v-btn :loading="busy" color="error" @click="remove">{{ t('zapretCatalog.lua.delete') }}</v-btn>
        </v-card-actions>
      </v-card>
    </v-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { catalogError, loadLuaDrafts, saveLuaDraft, type LuaDraft, type LuaDraftView, type LuaOrigin } from './catalog'

const props = defineProps<{ search: string | null; catalogRevision: string; storageLimit: number; disabled: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const { t } = useI18n()
const origins: LuaOrigin[] = ['administrator', 'zapretgui', 'external']
const originChoices = computed(() => origins.map(value => ({ value, title: t(`zapretCatalog.lua.origins.${value}`) })))
const snapshot = ref<LuaDraftView>()
const accessMessage = ref('')
const dialogError = ref('')
const busy = ref(false)
const editor = ref<{ mode: 'create' | 'edit' | 'import'; id: string; draft: LuaDraft; revision: string; file?: string }>()
const deleting = ref<{ id: string; revision: string }>()
const fileField = ref<HTMLInputElement>()
const drafts = computed(() => Object.entries(snapshot.value?.drafts ?? {}).sort(([a], [b]) => a.localeCompare(b)))
const filteredDrafts = computed(() => {
  const query = props.search?.trim().toLocaleLowerCase() ?? ''
  return drafts.value.filter(([id, draft]) => [id, draft.sourceRef ?? '', t(`zapretCatalog.lua.origins.${draft.origin}`)]
    .some(value => value.toLocaleLowerCase().includes(query)))
})
const canSave = computed(() => {
  const form = editor.value
  return !!form?.id.trim() && (form.mode !== 'import' || !!form.file) && (form.draft.origin === 'administrator' || !!form.draft.sourceRef?.trim())
})

async function refresh() {
  if (busy.value) return
  busy.value = true
  accessMessage.value = ''
  try { snapshot.value = await loadLuaDrafts() }
  catch (error) {
    snapshot.value = undefined
    accessMessage.value = (error as { response?: { status?: number } })?.response?.status === 403
      ? t('zapretCatalog.lua.adminOnly') : catalogError(error, t('zapretCatalog.lua.unavailable'))
  } finally { busy.value = false }
}
onMounted(refresh)
watch(() => props.catalogRevision, revision => {
  if (revision !== snapshot.value?.revision && !editor.value && !deleting.value) void refresh()
})

function openDraft(mode: 'create' | 'edit' | 'import', id = '', item?: LuaDraft) {
  if (!snapshot.value) return
  dialogError.value = ''
  editor.value = { mode, id, revision: snapshot.value.revision, draft: item ? { ...item } : { origin: mode === 'import' ? 'external' : 'administrator', source: '' } }
}
function openDelete(id: string) {
  if (!snapshot.value) return
  dialogError.value = ''
  deleting.value = { id, revision: snapshot.value.revision }
}
function editSource(value: string) {
  if (!editor.value) return
  const previous = editor.value.draft.source
  // A textarea normalizes line endings. Keep an imported CRLF convention on edit.
  editor.value.draft.source = previous.includes('\r\n') && !/(?<!\r)\n/.test(previous)
    ? value.replace(/\r?\n/g, '\r\n') : value
}
async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  dialogError.value = ''
  try { await action() }
  catch (error) { dialogError.value = catalogError(error, t('zapretCatalog.lua.unavailable')) }
  finally { busy.value = false }
}
function readFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  const form = editor.value
  input.value = ''
  if (!file || !form) return
  return run(async () => {
    if (!/\.lua$/i.test(file.name)) throw new Error(t('zapretCatalog.lua.fileType'))
    if (file.size > props.storageLimit) throw new Error(t('zapretCatalog.importTooLarge', { max: props.storageLimit / 1024 }))
    try { form.draft.source = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(await file.arrayBuffer()) }
    catch { throw new Error(t('zapretCatalog.lua.fileType')) }
    form.file = file.name
    if (!form.id) form.id = file.name.slice(0, -4).toLowerCase().replace(/[^a-z0-9-]+/g, '-').slice(0, 64).replace(/^[^a-z0-9]+|[^a-z0-9]+$/g, '')
  })
}
function downloadSource(id: string, source: string) {
  const blob = new Blob([source], { type: 'text/plain;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `${id.trim().replace(/[^a-zA-Z0-9_-]+/g, '-') || 'draft'}.lua`
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
const save = () => run(async () => {
  const form = editor.value
  if (!form || !canSave.value) return
  if (new TextEncoder().encode(form.draft.source).length > props.storageLimit) {
    throw new Error(t('zapretCatalog.importTooLarge', { max: props.storageLimit / 1024 }))
  }
  snapshot.value = await saveLuaDraft(form.mode === 'edit' ? 'edit' : 'create', form.id.trim(), form.revision,
    { ...form.draft, sourceRef: form.draft.sourceRef?.trim() })
  editor.value = undefined
  emit('changed')
})
const remove = () => run(async () => {
  const target = deleting.value
  if (!target) return
  snapshot.value = await saveLuaDraft('delete', target.id, target.revision)
  deleting.value = undefined
  emit('changed')
})
</script>

<style scoped>
.lua-source :deep(textarea) { font-family: ui-monospace, monospace; }
.lua-actions { max-width: 7rem; }
</style>
