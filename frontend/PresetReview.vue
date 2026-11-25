<template>
  <v-dialog :model-value="true" max-width="760" persistent>
    <v-card :title="template ? t('zapretCatalog.reviewTemplate', { name: templateLabel }) : t('zapretCatalog.import')">
      <v-card-text>
        <v-alert v-if="error" type="error" variant="tonal" class="mb-3" role="alert">{{ error }}</v-alert>
        <p v-if="template" class="text-body-2 mb-3">{{ template.origin }} {{ template.version }} · {{ template.license }}</p>
        <v-text-field v-model="name" :label="t('zapretCatalog.importName')" maxlength="120" density="compact" :disabled="busy" />
        <template v-if="!template">
          <input ref="fileField" type="file" accept=".json,.zip,.txt,.conf,.args" hidden @change="chooseFile" />
          <div class="d-flex flex-wrap align-center ga-2 mb-3">
            <v-btn variant="outlined" prepend-icon="mdi-upload" :disabled="busy" @click="fileField?.click()">{{ t('zapretCatalog.chooseFile') }}</v-btn>
            <span v-if="file" class="text-body-2 text-break">{{ file.name }}</span>
            <v-btn v-if="file" variant="text" :disabled="busy" @click="file = undefined">{{ t('zapretCatalog.clearFile') }}</v-btn>
          </div>
          <v-textarea v-if="!file" v-model="native" :label="t('zapretCatalog.raw')" rows="4" spellcheck="false"
            :maxlength="nativeReviewBytes" :disabled="busy" class="native-input" />
        </template>
        <v-btn variant="tonal" prepend-icon="mdi-check" :loading="busy && !result" :disabled="busy || !canReview" @click="review">
          {{ t('zapretCatalog.review') }}
        </v-btn>
        <section v-if="result" class="mt-4" aria-live="polite" aria-labelledby="zapret-review-result">
          <h3 id="zapret-review-result" class="text-subtitle-1 mb-2">{{ t('zapretCatalog.reviewResult') }}</h3>
          <v-alert :type="result.record ? 'success' : 'warning'" variant="tonal" class="mb-3">
            {{ t(result.record ? 'zapretCatalog.reviewValid' : 'zapretCatalog.reviewBlocked') }}
          </v-alert>
          <PresetOriginDetails v-if="result.origin" :origin="result.origin" />
          <ul v-if="result.issues?.length" class="ps-5 mb-3">
            <li v-for="(issue, i) in result.issues" :key="i" class="text-break mb-1">
              <strong v-if="issue.span">{{ t('zapretCatalog.sourceLocation', issue.span) }}: </strong>
              <code v-if="issue.option">{{ issue.option }}</code> {{ issue.message }}
            </li>
          </ul>
          <h4 v-if="orderedProfiles.length" class="text-subtitle-2">{{ t('zapretCatalog.visual') }}</h4>
          <ol class="ps-5 mb-2">
            <li v-for="(profile, i) in orderedProfiles" :key="i" class="text-break">{{ profile }}</li>
          </ol>
          <template v-if="result.dependencies?.length">
            <h4 class="text-subtitle-2">{{ t('zapretCatalog.dependencies') }}</h4>
            <ul class="ps-5 mb-3">
              <li v-for="(dep, i) in result.dependencies" :key="i" class="text-break">
                <code>{{ dep.source }}</code> → <code>{{ dep.id }}</code> · {{ t(`zapretCatalog.dependencyKind.${dep.kind}`, dep.kind) }} · {{ t(`zapretCatalog.dependencyState.${dep.state}`) }}
              </li>
            </ul>
          </template>
          <details v-if="result.source" class="mb-2">
            <summary>{{ t('zapretCatalog.originalSource') }}</summary>
            <pre>{{ result.source }}</pre>
          </details>
          <details v-if="result.record && result.converted" class="mb-2">
            <summary>{{ t('zapretCatalog.convertedSource') }}</summary>
            <pre>{{ result.record.raw }}</pre>
          </details>
          <details v-if="template" class="text-caption">
            <summary>{{ t('zapretCatalog.templateSource') }}</summary>
            <div class="text-break"><code>{{ template.sourcePath }}</code></div>
            <div class="text-break"><code>{{ template.commit }}</code></div>
          </details>
        </section>
      </v-card-text>
      <v-card-actions class="d-flex flex-wrap">
        <v-spacer />
        <v-btn :disabled="busy" @click="emit('close')">{{ t('zapretCatalog.cancel') }}</v-btn>
        <v-btn color="primary" variant="tonal" prepend-icon="mdi-content-save-outline" :loading="busy && !!result" :disabled="busy || !result?.record"
          @click="save">{{ t('zapretCatalog.savePreset') }}</v-btn>
      </v-card-actions>
    </v-card>
  </v-dialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PresetOriginDetails from './PresetOriginDetails.vue'
import { catalogError, encodePresetBytes, nativeReviewBytes, presetBytes, reviewPreset, savePreset, type CatalogView, type PresetInput, type PresetPreview, type PresetTemplate } from './catalog'

const props = defineProps<{ revision: string; template?: PresetTemplate; templateLabel?: string }>()
const emit = defineEmits<{ close: []; saved: [CatalogView] }>()
const { t } = useI18n()
const name = ref(props.templateLabel ?? '')
const native = ref('')
const file = ref<File>()
const fileField = ref<HTMLInputElement>()
const result = ref<PresetPreview>()
const reviewed = ref<PresetInput>()
const busy = ref(false)
const error = ref('')
const canReview = computed(() => props.template || file.value || native.value.trim())
const orderedProfiles = computed(() => result.value?.record?.definition.profiles.map(profile =>
  `${profile.id} · ${profile.protocol.toUpperCase()} · ${profile.ports.join(', ')}${profile.payload ? ` · ${profile.payload}` : ''}`)
  ?? result.value?.profiles?.map(profile => `${profile.name || t('zapretCatalog.unnamedProfile')} · ${t('zapretCatalog.sourceLine', profile)}`) ?? [])

function chooseFile(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files?.[0]) file.value = input.files[0]
  input.value = ''
}
watch([name, native, file], () => { result.value = undefined; reviewed.value = undefined; error.value = '' })
async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try { await action() } catch (err) { error.value = catalogError(err, t('zapretCatalog.unavailable')) }
  finally { busy.value = false }
}
const review = () => run(async () => {
  const label = name.value.trim()
  let input: PresetInput
  if (props.template) {
    input = { format: 'template', templateId: props.template.id, ...(label ? { name: label } : {}) }
  } else {
    const selected = file.value
    const suffix = selected?.name.split('.').at(-1)?.toLowerCase()
    const format = suffix === 'zip' ? 'zip' : suffix === 'json' ? 'json' : 'native'
    const max = format === 'native' ? nativeReviewBytes : presetBytes
    if (selected && selected.size > max) throw new Error(t('zapretCatalog.importTooLarge', { max: max / 1024 }))
    const content = selected ? new Uint8Array(await selected.arrayBuffer()) : new TextEncoder().encode(native.value)
    if (content.length > max) throw new Error(t('zapretCatalog.importTooLarge', { max: max / 1024 }))
    input = { format, name: label || (format === 'native' ? selected?.name.replace(/\.[^.]+$/, '') : undefined), content: encodePresetBytes(content) }
  }
  result.value = undefined
  reviewed.value = undefined
  result.value = await reviewPreset(input, props.revision)
  reviewed.value = input
})
const save = () => run(async () => {
  if (!result.value?.record || !reviewed.value) return
  emit('saved', await savePreset(reviewed.value, props.revision))
})
onMounted(() => { if (props.template) void review() })
</script>

<style scoped>
pre { max-height: 15rem; overflow: auto; overflow-wrap: anywhere; white-space: pre-wrap; margin-top: .5rem; }
.native-input :deep(textarea) { font-family: ui-monospace, monospace; }
</style>
