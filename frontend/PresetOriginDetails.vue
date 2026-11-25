<template>
  <details class="text-caption mb-2">
    <summary>{{ t(origin.templateId ? 'zapretCatalog.savedTemplateOrigin' : 'zapretCatalog.archiveOrigin') }}</summary>
    <div v-if="origin.templateId" class="text-break"><code>{{ origin.templateId }}</code> &middot; ZapretGUI {{ origin.referenceVersion }}</div>
    <div v-else class="text-break">{{ origin.fileName }} &middot; {{ origin.engine }} &middot; v{{ origin.version }}</div>
    <div v-if="origin.referenceCommit" class="text-break">Git: <code>{{ origin.referenceCommit }}</code></div>
    <div class="text-break">SHA-256: <code>{{ origin.sha256 }}</code></div>
  </details>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PresetOrigin } from './catalog'

defineProps<{ origin: PresetOrigin }>()
const { t } = useI18n()
</script>
