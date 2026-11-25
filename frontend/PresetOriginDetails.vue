<template>
  <details class="text-caption mb-2">
    <summary>{{ t('zapretCatalog.archiveOrigin') }}</summary>
    <div class="text-break">{{ origin.fileName }} &middot; {{ origin.engine }} &middot; v{{ origin.version }}</div>
    <div class="text-break">SHA-256: <code>{{ origin.sha256 }}</code></div>
  </details>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { PresetOrigin } from './catalog'

defineProps<{ origin: PresetOrigin }>()
const { t } = useI18n()
</script>
