<template>
  <v-container fluid class="zapret-catalog">
    <header v-if="!editing" class="d-flex flex-wrap align-center justify-space-between ga-3 mb-4">
      <h1 class="text-h5">{{ t('zapretCatalog.title') }}</h1>
      <div class="d-flex flex-wrap ga-2">
        <v-btn prepend-icon="mdi-refresh" variant="text" :disabled="busy" @click="refresh">{{ t('zapretCatalog.refresh') }}</v-btn>
        <v-btn prepend-icon="mdi-upload" variant="tonal" :disabled="busy || !view" @click="openImport()">{{ t('zapretCatalog.import') }}</v-btn>
        <v-btn prepend-icon="mdi-plus" color="primary" variant="flat" :disabled="busy || !view" @click="openDraft()">{{ t('zapretCatalog.create') }}</v-btn>
      </div>
    </header>
    <v-alert v-if="error" type="error" variant="tonal" class="mb-3">{{ error }}</v-alert>
    <v-progress-linear v-if="busy" indeterminate />
    <template v-if="view && !editing">
      <section class="host-summary mb-4" :aria-label="t('zapretCatalog.overview')">
        <dl class="summary-facts">
          <div><dt>{{ t('zapretCatalog.host') }}</dt><dd>{{ hostLabel }}</dd></div>
          <div><dt>{{ t('zapretCatalog.component') }}</dt><dd>{{ t(view.ready ? 'zapretCatalog.started' : 'zapretCatalog.unavailableShort') }}</dd></div>
          <div><dt>{{ t('zapretCatalog.engine') }}</dt><dd :title="view.engineCommit">{{ view.engineVersion ?? t('zapretCatalog.unknown') }}</dd></div>
          <div><dt>{{ t('zapretCatalog.activeBindings') }}</dt><dd>{{ appliedCount }} / {{ enabledCount }}</dd></div>
        </dl>
        <div class="d-flex flex-wrap align-center ga-2">
          <v-btn variant="text" size="small" prepend-icon="mdi-open-in-new" :to="hostRoute('pages.outbounds')">{{ t('zapretCatalog.outbounds') }}</v-btn>
          <v-btn variant="text" size="small" prepend-icon="mdi-pulse" @click="section = 'runtime'">{{ t('zapretCatalog.runtime') }}</v-btn>
          <v-btn variant="text" size="small" prepend-icon="mdi-cog" :to="hostRoute('pages.deployment')">{{ t('zapretCatalog.manage') }}</v-btn>
          <v-btn v-if="unverifiedCount" color="warning" variant="tonal" size="small" prepend-icon="mdi-alert-circle-outline" @click="section = 'runtime'">{{ t('zapretCatalog.unverifiedCount', { count: unverifiedCount }) }}</v-btn>
        </div>
      </section>
      <v-tabs v-model="section" density="compact" class="mb-4" :aria-label="t('zapretCatalog.views')">
        <v-tab value="strategies">{{ t('zapretCatalog.strategies') }}</v-tab>
        <v-tab value="assets">{{ t('zapretCatalog.assets') }}</v-tab>
        <v-tab value="runtime">{{ t('zapretCatalog.runtime') }}</v-tab>
      </v-tabs>
      <section v-if="section === 'strategies'" :aria-label="t('zapretCatalog.strategies')">
        <div class="catalog-tools mb-4">
          <v-btn-toggle v-model="collection" mandatory density="compact" :aria-label="t('zapretCatalog.collections')">
            <v-btn value="saved">{{ t('zapretCatalog.saved') }}</v-btn>
            <v-btn value="templates">{{ t('zapretCatalog.templates') }}</v-btn>
          </v-btn-toggle>
          <v-text-field v-model="search" :label="t('zapretCatalog.search')" prepend-inner-icon="mdi-magnify" clearable density="compact" hide-details />
        </div>
        <v-list v-if="collection === 'saved'" lines="two" class="border rounded-lg mb-4" :aria-label="t('zapretCatalog.saved')">
      <v-list-item v-for="record in filteredRecords" :key="record.definition.id" :title="record.name">
        <template #append>
          <div class="record-actions">
            <v-btn icon="mdi-pencil-outline" size="small" variant="text" :title="t('zapretCatalog.edit')" :aria-label="t('zapretCatalog.edit')" :disabled="busy" @click="openDraft(record)" />
            <v-btn icon="mdi-content-copy" size="small" variant="text" :title="t('zapretCatalog.clone')" :aria-label="t('zapretCatalog.clone')" :disabled="busy" @click="openDraft(record, true)" />
            <v-btn icon="mdi-download" size="small" variant="text" :title="t('zapretCatalog.export')" :aria-label="t('zapretCatalog.export')" :disabled="busy" @click="download(record, 'json')" />
            <v-menu>
              <template #activator="{ props }">
                <v-btn v-bind="props" icon="mdi-dots-vertical" size="small" variant="text" :title="t('zapretCatalog.exportFormats')" :aria-label="t('zapretCatalog.exportFormats')" :disabled="busy" />
              </template>
              <v-list density="compact">
                <v-list-item :title="t('zapretCatalog.exportZip')" @click="download(record, 'zip')" />
                <v-list-item :title="t('zapretCatalog.exportNative')" :disabled="hasManagedDependencies(record)" @click="download(record, 'native')" />
              </v-list>
            </v-menu>
            <v-btn icon="mdi-delete-outline" size="small" variant="text" :title="t('zapretCatalog.delete')" :aria-label="t('zapretCatalog.delete')" :disabled="busy || refsFor(record.definition.id).length > 0" @click="deleteRecord = record" />
          </div>
        </template>
        <div class="record-meta text-caption">
          {{ t('zapretCatalog.profileCount', { count: record.definition.profiles.length }) }} · {{ transports(record) }} ·
          {{ t(isNativeOnly(record) ? 'zapretCatalog.nativeOnlyLabel' : 'zapretCatalog.visual') }} · {{ recordStatus(record) }}
        </div>
        <div v-if="refsFor(record.definition.id).length" class="record-meta text-caption mt-1">
          {{ t('zapretCatalog.references') }} ({{ refsFor(record.definition.id).length }}): {{ refsFor(record.definition.id).map(ref => ref.outboundRef).join(', ') }}
        </div>
      </v-list-item>
      <v-list-item v-if="!filteredRecords.length" :title="t(records.length ? 'zapretCatalog.noMatches' : 'zapretCatalog.empty')" />
    </v-list>
      <template v-else>
        <div class="catalog-tools mb-3">
          <v-select v-model="category" :items="categories" :label="t('zapretCatalog.category')" density="compact" hide-details />
          <v-checkbox v-model="favoritesOnly" :label="t('zapretCatalog.favorites')" density="compact" hide-details />
        </div>
        <section v-for="group in templateGroups" :key="group.category" class="mb-4" :aria-label="categoryLabel(group.category)">
          <h2 class="text-subtitle-1 font-weight-medium mb-2">{{ categoryLabel(group.category) }}</h2>
          <div class="template-grid">
            <v-card v-for="template in group.templates" :key="template.id" variant="outlined" rounded="lg">
              <v-card-title class="template-title d-flex justify-space-between align-center ga-2">
                <span>{{ templateName(template) }}</span>
                <v-btn icon="mdi-heart-outline" size="small" :variant="favorites.includes(template.id) ? 'tonal' : 'text'"
                  :color="favorites.includes(template.id) ? 'primary' : undefined"
                  :title="t('zapretCatalog.favorite')" :aria-label="t('zapretCatalog.favorite')" :aria-pressed="favorites.includes(template.id)"
                  @click="toggleFavorite(template.id)" />
              </v-card-title>
              <v-card-text>
                <v-chip size="small" :color="template.compatibility === 'adapted' ? 'primary' : 'warning'" variant="tonal">
                  {{ t(`zapretCatalog.templateState.${template.compatibility}`) }}
                </v-chip>
                <p class="mt-2 mb-2">{{ templateDescription(template) }}</p>
                <div class="text-caption text-medium-emphasis">{{ template.origin }} {{ template.version }} · {{ t('zapretCatalog.profileCount', { count: template.profiles }) }}</div>
              </v-card-text>
              <v-card-actions><v-btn variant="tonal" :disabled="busy" @click="openImport(template)">{{ t('zapretCatalog.review') }}</v-btn></v-card-actions>
            </v-card>
          </div>
        </section>
        <p v-if="!templateGroups.length" class="text-body-2">{{ t('zapretCatalog.noMatches') }}</p>
      </template>
      </section>
    <section v-if="section === 'assets'" class="mb-4" :aria-label="t('zapretCatalog.assets')">
      <div class="catalog-tools mb-3">
        <v-text-field v-model="assetSearch" :label="t('zapretCatalog.searchAssets')" prepend-inner-icon="mdi-magnify" clearable density="compact" hide-details />
        <p v-if="view.storageLimit" class="text-caption my-2">{{ t('zapretCatalog.storage', { used: view.storageUsed ?? 0, limit: view.storageLimit }) }}</p>
      </div>
      <p class="text-body-2 text-medium-emphasis mb-3">{{ t('zapretCatalog.nativeFilterScope') }}</p>
      <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-2">
        <h2 class="text-subtitle-1">{{ t('zapretCatalog.sharedLists') }}</h2>
        <v-btn prepend-icon="mdi-plus" variant="tonal" :disabled="busy" @click="openList()">{{ t('zapretCatalog.newList') }}</v-btn>
      </div>
      <v-list density="compact" class="border rounded-lg mb-4">
        <v-list-item v-for="[id, list] in filteredLists" :key="id" :title="id" :subtitle="`${t(`zapretCatalog.${list.kind}Kind`)} · ${list.entries.length}`">
          <template #append>
            <v-btn icon="mdi-pencil-outline" variant="text" :aria-label="t('zapretCatalog.editList')" :title="t('zapretCatalog.editList')" :disabled="busy" @click="openList(id, list)" />
            <v-btn icon="mdi-delete-outline" variant="text" :aria-label="t('zapretCatalog.deleteList')" :title="t('zapretCatalog.deleteList')" :disabled="busy || listConsumers(id).length > 0" @click="deletingAsset = { id, blob: false }" />
          </template>
          <div v-if="listConsumers(id).length" class="text-caption text-break mt-1">{{ t('zapretCatalog.references') }}: {{ listConsumers(id).map(record => record.name).join(', ') }}</div>
          <details class="text-caption mt-1"><summary>{{ t('zapretCatalog.listEntries') }}</summary><div class="text-break">{{ list.entries.join(', ') }}</div></details>
        </v-list-item>
        <v-list-item v-if="!filteredLists.length" :title="t(namedLists.length ? 'zapretCatalog.noMatches' : 'zapretCatalog.noLists')" />
      </v-list>
      <div class="d-flex flex-wrap align-center justify-space-between ga-2 mb-2">
        <h2 class="text-subtitle-1">{{ t('zapretCatalog.sharedBlobs') }}</h2>
        <v-btn prepend-icon="mdi-plus" variant="tonal" :disabled="busy" @click="openList('', undefined, '')">{{ t('zapretCatalog.newBlob') }}</v-btn>
      </div>
      <v-list density="compact" class="border rounded-lg">
        <v-list-item v-for="[id, data] in filteredBlobs" :key="id" :title="id" :subtitle="t('zapretCatalog.blobSize', { count: data.length / 2 })">
          <template #append>
            <v-btn icon="mdi-pencil-outline" variant="text" :aria-label="t('zapretCatalog.editBlob')" :title="t('zapretCatalog.editBlob')" :disabled="busy" @click="openList(id, undefined, data)" />
            <v-btn icon="mdi-delete-outline" variant="text" :aria-label="t('zapretCatalog.deleteBlob')" :title="t('zapretCatalog.deleteBlob')" :disabled="busy || blobConsumers(id).length > 0" @click="deletingAsset = { id, blob: true }" />
          </template>
          <div v-if="blobConsumers(id).length" class="text-caption text-break mt-1">{{ t('zapretCatalog.references') }}: {{ blobConsumers(id).map(record => record.name).join(', ') }}</div>
        </v-list-item>
        <v-list-item v-if="!filteredBlobs.length" :title="t(namedBlobs.length ? 'zapretCatalog.noMatches' : 'zapretCatalog.noBlobs')" />
      </v-list>
      <LuaDrafts :key="hostID" :search="assetSearch" :catalog-revision="view.revision" :storage-limit="view.storageLimit ?? presetBytes"
        :disabled="busy" class="mt-5" @changed="refresh" />
    </section>
    <section v-if="section === 'runtime'" aria-labelledby="zapret-binding-status">
      <h2 id="zapret-binding-status" class="text-subtitle-1 mb-2">{{ t('zapretCatalog.bindings') }}</h2>
      <v-alert v-if="!view.ready" type="warning" variant="tonal" class="mb-3">{{ t('zapretCatalog.unavailable') }}</v-alert>
      <details class="text-body-2 mb-3">
        <summary>{{ t('zapretCatalog.platformScope') }}</summary>
        <p class="mt-1">{{ t('zapretCatalog.platformSystemd') }}</p>
        <p class="mt-1">{{ t('zapretCatalog.platformOpenWrt') }}</p>
      </details>
      <p v-if="!view.references.length" class="text-body-2">{{ t('zapretCatalog.noBindings') }}</p>
      <v-table v-else density="compact" class="runtime-table border rounded-lg">
        <thead><tr><th>{{ t('zapretCatalog.egress') }}</th><th>{{ t('zapretCatalog.strategy') }}</th><th>{{ t('zapretCatalog.status') }}</th><th>{{ t('zapretCatalog.diagnostics') }}</th></tr></thead>
        <tbody>
          <tr v-for="ref in view.references" :key="ref.outboundRef">
            <td class="text-break runtime-name">{{ ref.outboundRef }}</td>
            <td class="text-break runtime-name">{{ view.catalog.records[ref.strategyRef]?.name ?? ref.strategyRef }}</td>
            <td class="py-2">
              <v-chip :color="stateColors[ref.state]" size="small" variant="tonal" class="runtime-state">{{ t(`zapretCatalog.states.${ref.state}`) }}</v-chip>
              <div v-if="ref.state === 'applied'" class="text-caption text-break">
                <template v-if="ref.nativeMatches?.profiles?.length">
                  {{ t('zapretCatalog.observedProfiles') }}: <code>{{ ref.nativeMatches.profiles.join(', ') }}</code>
                </template>
                <template v-else>{{ t('zapretCatalog.noObservedProfiles') }}</template>
              </div>
              <details v-if="ref.desiredRevision || ref.appliedRevision || ref.queue" class="text-caption mt-1">
                <summary>{{ t('zapretCatalog.runtimeDetails') }}</summary>
                <div v-if="ref.desiredRevision">{{ t('zapretCatalog.desiredRevision') }}: <code :title="ref.desiredRevision">{{ ref.desiredRevision.slice(0, 12) }}</code></div>
                <div v-if="ref.appliedRevision">{{ t('zapretCatalog.appliedRevision') }}: <code :title="ref.appliedRevision">{{ ref.appliedRevision.slice(0, 12) }}</code></div>
                <div v-if="ref.queue">NFQUEUE: <code>{{ ref.queue }}</code></div>
              </details>
            </td>
            <td>
              <div class="d-flex">
                <v-btn icon="mdi-pulse" size="small" variant="text" :title="t('zapretCatalog.diagnostic')" :aria-label="t('zapretCatalog.diagnostic')" :disabled="busy || ref.state !== 'applied'" @click="runDiagnostic(ref)" />
                <v-btn icon="mdi-content-copy" size="small" variant="text" :title="t('zapretCatalog.copyEvidence')" :aria-label="t('zapretCatalog.copyEvidence')" :disabled="busy" @click="copyEvidence(ref)" />
              </div>
            </td>
          </tr>
        </tbody>
      </v-table>
      <v-alert v-if="diagnostic" :title="t('zapretCatalog.diagnosticResult')" :type="diagnostic.ok ? 'success' : 'warning'" variant="tonal" class="mt-3" role="status">
        <div>{{ diagnostic.text }}</div>
        <div class="text-caption mt-1">
          {{ diagnostic.strategy }} · <time :datetime="diagnostic.time">{{ new Date(diagnostic.time).toLocaleString() }}</time>
        </div>
      </v-alert>
      <v-alert v-if="copiedEvidence" type="info" variant="tonal" class="mt-3" role="status">{{ t('zapretCatalog.evidenceCopied') }}</v-alert>
    </section>
    </template>
    <section v-if="editing" ref="editorPage" aria-labelledby="zapret-editor-heading" data-testid="zapret-strategy-editor">
        <div class="d-flex flex-wrap align-center ga-2 mb-2">
          <v-btn variant="text" size="small" prepend-icon="mdi-arrow-left" :disabled="busy" @click="cancelDraft">{{ t('zapretCatalog.strategies') }}</v-btn>
          <v-icon icon="mdi-chevron-right" size="small" aria-hidden="true" />
          <span class="text-caption text-break">{{ originalName || t('zapretCatalog.create') }}</span>
        </div>
        <header class="d-flex flex-wrap align-center justify-space-between ga-2 mb-3">
          <h1 id="zapret-editor-heading" ref="editorHeading" class="text-h5" tabindex="-1">{{ activeID ? t('zapretCatalog.edit') : t('zapretCatalog.create') }}</h1>
          <span class="text-caption text-medium-emphasis">{{ t('zapretCatalog.draftRevision') }} <code :title="editorRevision">{{ editorRevision.slice(0, 8) }}</code></span>
        </header>
        <div class="editor-body" :inert="busy">
          <v-alert v-if="dialogError" type="error" variant="tonal" class="mb-3">{{ dialogError }}</v-alert>
          <v-alert v-if="affected.length" type="warning" variant="tonal" class="mb-3">{{ t('zapretCatalog.editAffects', { refs: affected.map(ref => ref.outboundRef).join(', ') }) }}</v-alert>
          <v-text-field v-model="name" :label="t('zapretCatalog.name')" maxlength="120" :disabled="busy" />
          <PresetOriginDetails v-if="activeOrigin" :origin="activeOrigin" />
          <div v-if="!originalRaw && !activeID && mode === 'visual' && rows.length === 1 && !rows[0]?.payload" class="d-flex flex-wrap ga-2 mb-3">
            <v-btn size="small" variant="tonal" @click="preset('tls_client_hello', '443,853')">{{ t('zapretCatalog.tlsPreset') }}</v-btn>
            <v-btn size="small" variant="tonal" @click="preset('http_req', '80')">{{ t('zapretCatalog.httpPreset') }}</v-btn>
            <v-btn size="small" variant="tonal" @click="preset('quic_initial', '443,853')">{{ t('zapretCatalog.quicPreset') }}</v-btn>
          </div>
          <v-btn-toggle :model-value="mode" mandatory density="compact" class="mb-3" @update:model-value="switchMode">
            <v-btn value="visual" :disabled="busy">{{ t('zapretCatalog.visual') }}</v-btn>
            <v-btn value="raw" :disabled="busy">{{ t('zapretCatalog.raw') }}</v-btn>
          </v-btn-toggle>
          <template v-if="mode === 'raw'">
            <v-alert v-if="rawLocked" type="info" variant="tonal" density="compact" class="mb-3">{{ t('zapretCatalog.nativeOnlyHelp') }}</v-alert>
            <v-textarea v-model="raw" class="native-source" :label="t('zapretCatalog.raw')" spellcheck="false" maxlength="8192" rows="9" :disabled="busy" />
            <details v-if="rawListProfiles.length && (namedLists.length || Object.keys(rawRefs).length)" class="mb-3" :open="!!Object.keys(rawRefs).length">
              <summary>{{ t('zapretCatalog.sharedLists') }}</summary>
              <div v-for="id in rawListProfiles" :key="id" class="mt-2">
                <div class="text-caption font-weight-medium"><code>{{ id }}</code></div>
                <div class="list-fields mt-1">
                  <v-select v-for="field in listFields" :key="field" :model-value="rawRefs[id]?.[field] ?? null" :items="listChoices(field)"
                    :label="t('zapretCatalog.useList', { field: t(`zapretCatalog.${field}`) })" clearable hide-details density="compact"
                    @update:model-value="setRawListRef(id, field, $event)" />
                </div>
              </div>
            </details>
          </template>
          <template v-else>
            <div v-for="(row, index) in rows" :key="row.key" class="profile-row border rounded-lg pa-3 mb-2">
              <div class="d-flex justify-space-between align-center flex-wrap ga-2">
                <div class="text-break">
                  <strong>{{ t('zapretCatalog.profile', { index: index + 1 }) }}</strong>
                  <div class="text-caption text-medium-emphasis">{{ row.protocol === 'udp' ? 'UDP' : 'TCP' }} {{ row.ports }}<template v-if="row.protocol === 'tcp+udp'"> / UDP {{ row.udpPorts }}</template> · {{ row.family || t('zapretCatalog.availableFamilies') }}</div>
                </div>
                <div>
                  <v-btn icon="mdi-chevron-up" size="small" variant="text" :disabled="index === 0" :title="t('zapretCatalog.moveUp')" :aria-label="t('zapretCatalog.moveUp')" @click="move(index, -1)" />
                  <v-btn icon="mdi-chevron-down" size="small" variant="text" :disabled="index === rows.length - 1" :title="t('zapretCatalog.moveDown')" :aria-label="t('zapretCatalog.moveDown')" @click="move(index, 1)" />
                  <v-btn icon="mdi-close" size="small" variant="text" :disabled="rows.length <= 1" :title="t('zapretCatalog.removeProfile')" :aria-label="t('zapretCatalog.removeProfile')" @click="remove(index)" />
                </div>
              </div>
              <div class="profile-fields mt-2">
                <v-text-field v-model="row.id" :label="t('zapretCatalog.profileID')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-select :model-value="row.protocol" :items="protocols" :label="t('zapretCatalog.protocol')" hide-details density="compact" @update:model-value="changeProtocol(row, $event)" />
                <v-select v-model="row.family" :items="families" :label="t('zapretCatalog.family')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-text-field v-model="row.ports" :label="t(row.protocol === 'tcp+udp' ? 'zapretCatalog.tcpPorts' : 'zapretCatalog.ports')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-text-field v-if="row.protocol === 'tcp+udp'" v-model="row.udpPorts" :label="t('zapretCatalog.udpPorts')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <v-select :model-value="row.payload" :items="payloads(row.protocol)" :label="t('zapretCatalog.payload')" hide-details density="compact" @update:model-value="changePayload(row, $event)" />
                <v-text-field v-if="row.payload === 'quic_initial'" v-model="row.quicRepeats" type="number" min="1" max="4" :label="t('zapretCatalog.quicRepeats')" hide-details density="compact" @update:model-value="visualChanged = true" />
                <template v-else-if="row.payload">
                  <v-select :model-value="row.httpActions ? 'http' : row.circular ? 'circular' : 'split'" :items="tcpActions(row.payload)" :label="t('zapretCatalog.tcpAction')" hide-details density="compact" @update:model-value="changeTCPAction(row, $event)" />
                  <template v-if="row.circular">
                    <v-text-field v-model="row.circular.failures" type="number" min="1" max="255" :label="t('zapretCatalog.circularFailures')" hide-details density="compact" @update:model-value="visualChanged = true" />
                    <v-text-field v-model="row.circular.splits" :label="t('zapretCatalog.circularSplits')" hide-details density="compact" @update:model-value="visualChanged = true" />
                  </template>
                  <v-text-field v-else-if="!row.httpActions" v-model="row.splitAt" type="number" min="1" max="128" :label="t('zapretCatalog.split')" hide-details density="compact" @update:model-value="visualChanged = true" />
                </template>
              </div>
              <div v-if="row.httpActions" class="profile-fields mt-2">
                <v-select :model-value="row.httpActions.map(action => action.name)" :items="httpActions" multiple
                  :label="t('zapretCatalog.httpOrder')" hide-details density="compact" @update:model-value="changeHTTPActions(row, $event)" />
                <v-text-field v-if="row.httpActions.some(action => action.name === 'http_hostcase')" maxlength="4"
                  :model-value="row.httpActions.find(action => action.name === 'http_hostcase')?.spell ?? ''"
                  :label="t('zapretCatalog.httpSpell')" placeholder="host" spellcheck="false" hide-details density="compact"
                  @update:model-value="setHTTPSpell(row, $event)" />
                <v-btn v-if="row.httpActions.length > 1" icon="mdi-swap-vertical" size="small" variant="text"
                  :aria-label="t('zapretCatalog.httpReverse')" :title="t('zapretCatalog.httpReverse')" @click="reverseHTTPActions(row)" />
              </div>
              <v-select v-if="row.payload === 'quic_initial' && blobChoices.length > 1" v-model="row.quicBlobRef" :items="blobChoices"
                :label="t('zapretCatalog.quicBlob')" class="mt-2" hide-details density="compact" @update:model-value="visualChanged = true" />
              <details v-if="row.payload && !row.circular" class="mt-2"
                :open="row.rangeInitiallyOpen || undefined">
                <summary>{{ t('zapretCatalog.outRange') }}</summary>
                <div v-if="row.httpActions" class="profile-fields mt-2">
                  <v-text-field v-for="action in row.httpActions" :key="action.name" :model-value="action.outRange ?? ''"
                    :label="httpActions.find(item => item.value === action.name)?.title" placeholder="a" maxlength="23"
                    spellcheck="false" hide-details density="compact" @update:model-value="setActionOutRange(action, $event)" />
                </div>
                <v-text-field v-else v-model="row.outRange" :label="t('zapretCatalog.outRange')" placeholder="a" maxlength="23"
                  class="mt-2" spellcheck="false" hide-details density="compact" @update:model-value="visualChanged = true" />
              </details>
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
            <v-btn variant="text" prepend-icon="mdi-plus" @click="addProfile">{{ t('zapretCatalog.addProfile') }}</v-btn>
          </template>
          <div class="text-caption text-medium-emphasis">{{ t('zapretCatalog.assetScope') }}</div>
          <details v-if="effective" class="mt-3" open>
            <summary>{{ t('zapretCatalog.effective') }}</summary>
            <pre class="effective-preview">{{ effective.raw }}</pre>
          </details>
        </div>
        <footer class="editor-footer d-flex flex-wrap justify-end ga-2 mt-4">
          <v-btn :disabled="busy" variant="outlined" prepend-icon="mdi-check" @click="validateDraft">{{ t('zapretCatalog.validate') }}</v-btn>
          <v-btn :disabled="busy" variant="text" @click="cancelDraft">{{ t('zapretCatalog.cancel') }}</v-btn>
          <v-btn :loading="busy" :disabled="!name.trim()" color="primary" variant="flat" prepend-icon="mdi-content-save-outline" @click="save">{{ t('zapretCatalog.save') }}</v-btn>
        </footer>
    </section>
    <PresetReview v-if="importReview" :revision="importReview.revision" :template="importReview.template"
      :template-label="importReview.template ? templateName(importReview.template) : undefined" @close="importReview = undefined" @saved="presetSaved" />
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
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave, onBeforeRouteUpdate, useRoute } from 'vue-router'
import LuaDrafts from './LuaDrafts.vue'
import PresetReview from './PresetReview.vue'
import PresetOriginDetails from './PresetOriginDetails.vue'
import { catalogError, diagnose, draftToSave, exportRecord, loadCatalog, presetBytes, preview, saveCatalog, type CatalogView, type ExportFormat, type HTTPAction, type ListKind, type ListRefs, type NamedList, type NativeListField, type PresetTemplate, type Profile, type ProfileListRefs, type StrategyRecord, type StrategyReference } from './catalog'

const { t, te } = useI18n()
const route = useRoute()
const hostID = computed(() => typeof route.params.hostId === 'string' ? route.params.hostId : 'local')
const hostLabel = computed(() => hostID.value === 'local' ? t('zapretCatalog.localHost') : hostID.value)
const hostRoute = (name: string) => ({ name, params: { hostId: hostID.value } })
type RowProtocol = Profile['protocol'] | 'tcp+udp'
type Row = { key: number; id: string; protocol: RowProtocol; family: NonNullable<Profile['family']> | ''; ports: string; udpPorts: string;
  payload: NonNullable<Profile['payload']> | ''; splitAt: string; circular?: { failures: string; splits: string }; httpActions?: HTTPAction[];
  quicRepeats: string; quicBlobRef: string; outRange: string; rangeInitiallyOpen?: boolean; lists: Record<NativeListField, string>; refs: ProfileListRefs }
const listFields = ['hostList', 'hostListExclude', 'ipSet', 'ipSetExclude'] as const
const view = ref<CatalogView>()
const busy = ref(false)
const error = ref('')
const dialogError = ref('')
const diagnostic = ref<{ outbound: string; ok: boolean; text: string; time: string; strategy: string }>()
const copiedEvidence = ref(false)
const importReview = ref<{ revision: string; template?: PresetTemplate }>()
const section = ref<'strategies' | 'assets' | 'runtime'>('strategies')
const collection = ref<'saved' | 'templates'>('saved')
const search = ref<string | null>('')
const assetSearch = ref<string | null>('')
const category = ref('')
const favoritesOnly = ref(false)
const favorites = ref<string[]>([])
const deleteRecord = ref<StrategyRecord>()
const editing = ref(false)
const activeID = ref('')
const activeOrigin = computed(() => view.value?.catalog.records[activeID.value]?.origin)
const editorRevision = ref('')
const editorHeading = ref<HTMLElement>()
const editorPage = ref<HTMLElement>()
const originalName = ref('')
const originalRaw = ref('')
const effective = ref<StrategyRecord>()
const rawLocked = ref(false)
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
const rawProfileIDs = ref<string[]>([])
const rawRefsChanged = ref(false)
const rawListProfiles = computed(() => [...new Set([...rawProfileIDs.value, ...Object.keys(rawRefs.value)])])
const mode = ref<'visual' | 'raw'>('visual')
const rows = ref<Row[]>([])
const visualChanged = ref(false)
let rowKey = 0
const records = computed(() => Object.values(view.value?.catalog.records ?? {}).sort((a, b) => a.name.localeCompare(b.name)))
const namedLists = computed(() => Object.entries(view.value?.catalog.lists ?? {}).sort(([a], [b]) => a.localeCompare(b)))
const namedBlobs = computed(() => Object.entries(view.value?.catalog.blobAssets ?? {}).sort(([a], [b]) => a.localeCompare(b)))
const enabledCount = computed(() => view.value?.references.filter(ref => ref.enabled).length ?? 0)
const appliedCount = computed(() => view.value?.references.filter(ref => ref.state === 'applied').length ?? 0)
const unverifiedCount = computed(() => view.value?.references.filter(ref => ref.state === 'blocked').length ?? 0)
const stateColors: Record<StrategyReference['state'], string | undefined> = { applied: 'success', pending: 'warning', blocked: 'warning', off: undefined }
const matches = (term: string | null, ...values: string[]) => values.some(value => value.toLocaleLowerCase().includes(term?.trim().toLocaleLowerCase() ?? ''))
const profileTransports = (profile: Profile) => profile.udpPorts?.length ? ['tcp', 'udp'] : [profile.protocol]
const filteredRecords = computed(() => records.value.filter(record => matches(search.value, record.name, record.definition.id,
  ...record.definition.profiles.flatMap(profile => [profile.id, ...profileTransports(profile), profile.payload ?? '']))))
const filteredLists = computed(() => namedLists.value.filter(([id, list]) => matches(assetSearch.value, id, t(`zapretCatalog.${list.kind}Kind`), ...list.entries)))
const filteredBlobs = computed(() => namedBlobs.value.filter(([id]) => matches(assetSearch.value, id)))
const templates = computed(() => view.value?.templates ?? [])
const categoryLabel = (id: string) => t(`zapretCatalog.categories.${id}`, id)
const categories = computed(() => [{ title: t('zapretCatalog.allCategories'), value: '' }, ...[...new Set(templates.value.map(item => item.category))]
  .map(value => ({ title: categoryLabel(value), value }))])
const templateName = (item: PresetTemplate) => t(`zapretCatalog.templateDetails.${item.id}.name`, item.name)
const templateDescription = (item: PresetTemplate) => t(`zapretCatalog.templateDetails.${item.id}.description`, item.description)
const templateGroups = computed(() => categories.value.slice(1).map(({ value }) => ({
  category: value,
  templates: templates.value.filter(item => item.category === value && (!category.value || category.value === value)
    && (!favoritesOnly.value || favorites.value.includes(item.id)) && matches(search.value, templateName(item), templateDescription(item), item.name, item.id)),
})).filter(group => group.templates.length))
const preferenceStorage = computed(() => view.value?.preferenceKey ? `sui:zapret:favorites:${view.value.preferenceKey}` : '')
watch(preferenceStorage, key => {
  favorites.value = []
  if (!key) return
  try {
    const found: unknown = JSON.parse(window.localStorage.getItem(key) ?? '[]')
    if (Array.isArray(found)) favorites.value = templates.value.map(item => item.id).filter(id => found.includes(id))
  } catch { /* Browser storage is optional. */ }
})
function toggleFavorite(id: string) {
  favorites.value = favorites.value.includes(id) ? favorites.value.filter(value => value !== id) : [...favorites.value, id]
  try { if (preferenceStorage.value) window.localStorage.setItem(preferenceStorage.value, JSON.stringify(favorites.value)) } catch { /* Keep this page's preference. */ }
}
const blobChoices = computed(() => [{ title: t('zapretCatalog.defaultBlob'), value: '' }, ...namedBlobs.value.map(([id]) => ({ title: id, value: id }))])
const listKinds = computed<{ title: string; value: ListKind }[]>(() => [
  { title: t('zapretCatalog.hostListKind'), value: 'hostList' }, { title: t('zapretCatalog.ipSetKind'), value: 'ipSet' },
])
const listChoices = (field: NativeListField) => namedLists.value.filter(([, list]) => list.kind === (field.startsWith('hostList') ? 'hostList' : 'ipSet')).map(([id]) => id)
const listConsumers = (id: string) => records.value.filter(record => Object.values(record.listRefs ?? {}).some(refs => Object.values(refs).includes(id)))
const blobConsumers = (id: string) => records.value.filter(record => record.definition.profiles.some(p => p.quicBlobRef === id))
const families = computed(() => [{ title: t('zapretCatalog.availableFamilies'), value: '' }, { title: 'IPv4', value: 'ipv4' }, { title: 'IPv6', value: 'ipv6' }])
const protocols: { title: string; value: RowProtocol }[] = [
  { title: 'TCP', value: 'tcp' }, { title: 'UDP', value: 'udp' }, { title: 'TCP + UDP', value: 'tcp+udp' },
]
const payloads = (protocol: RowProtocol) => [{ title: t('zapretCatalog.passive'), value: '' },
  ...(protocol !== 'udp' ? [{ title: 'TLS ClientHello', value: 'tls_client_hello' }, { title: 'HTTP request', value: 'http_req' }] : []),
  ...(protocol !== 'tcp' ? [{ title: 'QUIC Initial', value: 'quic_initial' }] : [])]
const tcpActions = (payload: Row['payload']) => [
  { title: t('zapretCatalog.split'), value: 'split' }, { title: 'Circular', value: 'circular' },
  ...(payload === 'http_req' ? [{ title: t('zapretCatalog.httpTransforms'), value: 'http' }] : []),
]
const httpActions = computed(() => [
  { title: t('zapretCatalog.httpDomainCase'), value: 'http_domcase' },
  { title: t('zapretCatalog.httpHostCase'), value: 'http_hostcase' },
])
const refsFor = (id: string) => view.value?.references.filter(ref => ref.strategyRef === id) ?? []
const isNativeOnly = (record: StrategyRecord) => view.value?.nativeOnly?.includes(record.definition.id) ?? false
const hasManagedDependencies = (record: StrategyRecord) => !!Object.keys(record.listRefs ?? {}).length || record.definition.profiles.some(profile => !!profile.quicBlobRef)
const transports = (record: StrategyRecord) => [...new Set(record.definition.profiles.flatMap(profileTransports))].map(protocol => protocol.toUpperCase()).join(' / ')
function recordStatus(record: StrategyRecord) {
  const states = new Set(refsFor(record.definition.id).map(ref => ref.state))
  if (!states.size) return t('zapretCatalog.unassigned')
  const state = (['blocked', 'pending', 'applied', 'off'] as const).find(value => states.has(value))
  return t(`zapretCatalog.states.${state}`)
}
const affected = computed(() => refsFor(activeID.value).filter(ref => ref.enabled))
const listAffected = computed(() => listActiveID.value ? (editingBlob.value ? blobConsumers : listConsumers)(listActiveID.value)
  .flatMap(record => refsFor(record.definition.id).filter(ref => ref.enabled).map(ref => ref.outboundRef)) : [])
async function run(action: () => Promise<void>, inDialog = false) {
  if (busy.value) return
  busy.value = true
  error.value = dialogError.value = ''
  try { await action() } catch (err) { (inDialog ? dialogError : error).value = catalogError(err, t('zapretCatalog.unavailable')) }
  finally { busy.value = false }
}
const refresh = () => run(async () => { view.value = await loadCatalog(); diagnostic.value = undefined; copiedEvidence.value = false })
const dirty = computed(() => editing.value && (visualChanged.value || rawRefsChanged.value || name.value !== originalName.value || raw.value !== originalRaw.value))
const confirmDiscard = () => !dirty.value || window.confirm(t('zapretCatalog.discardDraft'))
const warnUnload = (event: BeforeUnloadEvent) => { if (dirty.value) event.preventDefault() }
onMounted(() => { window.addEventListener('beforeunload', warnUnload); void refresh() })
onUnmounted(() => window.removeEventListener('beforeunload', warnUnload))
onBeforeRouteLeave(confirmDiscard)
onBeforeRouteUpdate((to, from) => to.params.hostId === from.params.hostId || confirmDiscard())
watch([name, raw, rows, rawRefs], () => { effective.value = undefined }, { deep: true })
function listText(lists: Profile['lists'] = {}): Row['lists'] {
  return { hostList: lists.hostList?.join('\n') ?? '', hostListExclude: lists.hostListExclude?.join('\n') ?? '',
    ipSet: lists.ipSet?.join('\n') ?? '', ipSetExclude: lists.ipSetExclude?.join('\n') ?? '' }
}
function withRows(record?: StrategyRecord) {
  rawRefs.value = { ...record?.listRefs }
  rawProfileIDs.value = record?.definition.profiles.map(p => p.id) ?? []
  rows.value = (record?.definition.profiles ?? []).map(p => {
    const refs = { ...rawRefs.value[p.id] }
    const lists = listText(p.lists)
    for (const field of listFields) if (refs[field]) lists[field] = ''
    return { key: ++rowKey, id: p.id, protocol: p.udpPorts?.length ? 'tcp+udp' : p.protocol, family: p.family ?? '',
      ports: p.ports.join(','), udpPorts: p.udpPorts?.join(',') ?? '',
      payload: p.payload ?? '', splitAt: String(p.splitAt ?? 9), quicRepeats: String(p.quicRepeats ?? 1), quicBlobRef: p.quicBlobRef ?? '',
      outRange: p.outRange ?? '', rangeInitiallyOpen: !!p.outRange || p.httpActions?.some(action => !!action.outRange),
      ...(p.circular ? { circular: { failures: String(p.circular.failures), splits: p.circular.splits.join(',') } } : {}),
      ...(p.httpActions ? { httpActions: p.httpActions.map(action => ({ ...action })) } : {}), lists, refs }
  })
}
function setListRef(row: Row, field: NativeListField, value: string | null) {
  if (value) row.refs[field] = value
  else delete row.refs[field]
  visualChanged.value = true
}
function setRawListRef(profile: string, field: NativeListField, value: string | null) {
  const refs = { ...rawRefs.value[profile] }
  if (value) refs[field] = value
  else delete refs[field]
  if (Object.keys(refs).length) rawRefs.value[profile] = refs
  else delete rawRefs.value[profile]
  rawRefsChanged.value = true
}
function changeProtocol(row: Row, protocol: RowProtocol) {
  if (row.protocol === protocol) return
  if (row.protocol === 'udp' && protocol === 'tcp+udp') {
    row.udpPorts = row.ports
    row.ports = ''
  } else if (row.protocol === 'tcp+udp' && protocol === 'udp') {
    row.ports = row.udpPorts
  }
  if (protocol !== 'tcp+udp') row.udpPorts = ''
  row.protocol = protocol
  if ((protocol === 'tcp' && row.payload === 'quic_initial') || (protocol === 'udp' && row.payload !== 'quic_initial')) {
    changePayload(row, '')
  }
  visualChanged.value = true
}
function changePayload(row: Row, payload: Row['payload']) {
  row.payload = payload ?? ''
  if (payload !== 'http_req') delete row.httpActions
  if (payload !== 'tls_client_hello' && payload !== 'http_req') delete row.circular
  if (payload !== 'quic_initial') row.quicBlobRef = ''
  if (!payload) row.outRange = ''
  visualChanged.value = true
}
function changeTCPAction(row: Row, action: string) {
  if (action === 'circular') row.circular ??= { failures: '1', splits: '9,17' }
  else delete row.circular
  if (action === 'http') row.httpActions ??= [{ name: 'http_domcase' }, { name: 'http_hostcase' }]
  else delete row.httpActions
  if (action !== 'split') row.outRange = ''
  visualChanged.value = true
}
function changeHTTPActions(row: Row, names: HTTPAction['name'][]) {
  if (!names.length) return
  row.httpActions = names.map(name => row.httpActions?.find(action => action.name === name) ?? { name })
  visualChanged.value = true
}
function setHTTPSpell(row: Row, spell: string | null) {
  const action = row.httpActions?.find(action => action.name === 'http_hostcase')
  if (!action) return
  if (spell) action.spell = spell
  else delete action.spell
  visualChanged.value = true
}
function setActionOutRange(action: HTTPAction, value: string | null) {
  if (value) action.outRange = value
  else delete action.outRange
  visualChanged.value = true
}
function reverseHTTPActions(row: Row) {
  row.httpActions?.reverse()
  visualChanged.value = true
}
function addProfile() {
  let index = 1
  while (rows.value.some(row => row.id === `profile-${index}`)) index++
  rows.value.push({ key: ++rowKey, id: `profile-${index}`, protocol: 'tcp', family: '', ports: '443', udpPorts: '', payload: '', splitAt: '9', quicRepeats: '1', quicBlobRef: '', outRange: '', lists: listText(), refs: {} })
  visualChanged.value = true
}
function remove(index: number) { rows.value.splice(index, 1); visualChanged.value = true }
function move(index: number, offset: number) {
  const [row] = rows.value.splice(index, 1)
  rows.value.splice(index + offset, 0, row)
  visualChanged.value = true
}
function preset(payload: NonNullable<Profile['payload']>, ports: string) {
  rows.value = [{ key: ++rowKey, id: 'profile-1', protocol: payload === 'quic_initial' ? 'udp' : 'tcp', family: '', ports, udpPorts: '', payload,
    splitAt: '9', quicRepeats: '1', quicBlobRef: '', outRange: '', lists: listText(), refs: {} }]
  mode.value = 'visual'
  visualChanged.value = true
}
function openDraft(record?: StrategyRecord, clone = false) {
  activeID.value = clone ? '' : record?.definition.id ?? ''
  editorRevision.value = view.value?.revision ?? ''
  name.value = clone ? t('zapretCatalog.copyName', { name: record?.name }) : record?.name ?? ''
  raw.value = record?.raw ?? ''
  originalName.value = name.value
  originalRaw.value = raw.value
  rawLocked.value = !!record && isNativeOnly(record)
  mode.value = rawLocked.value ? 'raw' : 'visual'
  dialogError.value = ''
  withRows(record)
  rawRefsChanged.value = false
  if (!record) addProfile()
  visualChanged.value = false
  editing.value = true
  void nextTick(() => {
    editorPage.value?.scrollIntoView({ block: 'start' })
    editorHeading.value?.focus({ preventScroll: true })
  })
}
function cancelDraft() {
  if (!confirmDiscard()) return
  dialogError.value = ''
  editing.value = false
}
function draft() {
  if (mode.value === 'raw' || !visualChanged.value && raw.value) return draftToSave(name.value.trim(), raw.value, [], false, rawRefs.value)
  const profiles = rows.value.map<Profile>(row => {
    const lists: NonNullable<Profile['lists']> = {}
    for (const field of listFields) {
      const entries = row.lists[field].split(/[\s,]+/).filter(Boolean)
      if (entries.length) lists[field] = entries
    }
    return { id: row.id.trim(), protocol: row.protocol === 'udp' ? 'udp' : 'tcp', ports: parsePorts(row.ports),
      ...(row.protocol === 'tcp+udp' ? { udpPorts: parsePorts(row.udpPorts) } : {}),
      ...(row.payload && !row.circular && !row.httpActions && row.outRange ? { outRange: row.outRange } : {}),
      ...(row.family ? { family: row.family } : {}), ...(row.payload === 'quic_initial' ? { payload: row.payload, quicRepeats: Number(row.quicRepeats),
        ...(row.quicBlobRef ? { quicBlobRef: row.quicBlobRef } : {}) }
        : row.payload ? { payload: row.payload, ...(row.httpActions ? { httpActions: row.httpActions } : row.circular
          ? { circular: { failures: Number(row.circular.failures), splits: row.circular.splits.split(',').map(split => Number(split.trim())) } }
          : { splitAt: Number(row.splitAt) }) } : {}), ...(Object.keys(lists).length ? { lists } : {}) }
  })
  const refs = Object.fromEntries(rows.value.filter(row => Object.keys(row.refs).length).map(row => [row.id.trim(), row.refs]))
  return draftToSave(name.value.trim(), '', profiles, true, refs)
}
function parsePorts(input: string) {
  const ports: number[] = []
  for (const term of input.split(',')) {
    const field = /^(\d{1,5})(?:-(\d{1,5}))?$/.exec(term.trim())
    const first = Number(field?.[1] ?? 0)
    const last = Number(field?.[2] ?? first)
    if (first < 1 || last < first || last > 65535) throw new Error(t('zapretCatalog.portSyntax'))
    if (ports.length + last - first + 1 > 64) throw new Error(t('zapretCatalog.portBudget'))
    for (let port = first; port <= last; port++) ports.push(port)
  }
  return ports
}
const previewDraft = () => preview({ ...draft(), name: name.value.trim() || 'Preview' }, editorRevision.value)
const validateDraft = () => run(async () => {
  const record = await previewDraft()
  if (mode.value === 'raw') rawProfileIDs.value = record.definition.profiles.map(p => p.id)
  effective.value = record
}, true)
async function switchMode(next: 'visual' | 'raw') {
  if (next === mode.value) return
  await run(async () => {
    if (next === 'visual' || visualChanged.value || !raw.value) {
      const record = await previewDraft()
      rawLocked.value = record.visualEditable === false
      if (next === 'visual' && rawLocked.value) throw new Error(t('zapretCatalog.nativeOnlyHelp'))
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
  section.value = 'strategies'
  collection.value = 'saved'
  search.value = ''
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
  const reason = `runtime.controlResults.${result.Error}`
  diagnostic.value = { outbound: ref.outboundRef, ok: result.OK, time: new Date().toISOString(),
    strategy: view.value.catalog.records[ref.strategyRef]?.name ?? ref.strategyRef,
    text: result.OK ? t('zapretCatalog.testOK', { egress: ref.outboundRef, delay: result.Delay })
      : t('zapretCatalog.testFailed', { egress: ref.outboundRef, reason: te(reason) ? t(reason) : t('zapretCatalog.checkFailed') }) }
})
const copyEvidence = (ref: StrategyReference) => run(async () => {
  copiedEvidence.value = false
  await navigator.clipboard.writeText(JSON.stringify({
    component: 'zapret', engine: view.value?.engineVersion, state: ref.state,
    desiredRevision: ref.desiredRevision, appliedRevision: ref.appliedRevision,
    observedProfileCount: ref.state === 'applied' ? ref.nativeMatches?.profiles?.length ?? 0 : 0,
    ...(diagnostic.value?.outbound === ref.outboundRef ? { lastCheck: { ok: diagnostic.value.ok, time: diagnostic.value.time } } : {}),
  }, null, 2))
  copiedEvidence.value = true
})
const download = (record: StrategyRecord, format: ExportFormat) => run(async () => {
  if (!view.value) return
  const data = await exportRecord(record.definition.id, view.value.revision, format)
  const url = URL.createObjectURL(data)
  const link = document.createElement('a')
  link.href = url
  link.download = `${record.definition.id}.${format === 'native' ? 'txt' : format}`
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
})
function openImport(template?: PresetTemplate) {
  if (view.value) importReview.value = { revision: view.value.revision, template }
}
function presetSaved(next: CatalogView) {
  view.value = next
  importReview.value = undefined
  section.value = 'strategies'
  collection.value = 'saved'
  search.value = ''
}
</script>

<style scoped>
.zapret-catalog { max-width: 1120px; }
.host-summary { border-block: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); padding-block: .75rem; }
.summary-facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 140px), 1fr)); gap: .75rem; }
.summary-facts dt { font-size: .75rem; color: rgba(var(--v-theme-on-surface), .7); }
.summary-facts dd { font-weight: 600; overflow-wrap: anywhere; }
.catalog-tools { display: grid; align-items: center; grid-template-columns: minmax(0, max-content) minmax(0, 24rem); gap: .75rem; }
.template-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(100%, 16rem), 1fr)); gap: .75rem; }
.template-title { white-space: normal; overflow-wrap: anywhere; font-size: 1rem; }
.record-actions { display: flex; flex-wrap: wrap; }
.record-meta { overflow-wrap: anywhere; }
.runtime-name { min-width: 6.5rem; }
.runtime-state { max-width: 100%; }
.editor-body { min-inline-size: 0; }
.editor-footer { position: sticky; bottom: 0; z-index: 1; background: rgb(var(--v-theme-background)); padding: .5rem 0; }
.effective-preview { max-height: 18rem; overflow: auto; overflow-wrap: anywhere; white-space: pre-wrap; }
.profile-fields { display: grid; gap: 8px; grid-template-columns: repeat(auto-fit, minmax(min(100%, 165px), 1fr)); }
.list-fields { display: grid; gap: 8px; grid-template-columns: repeat(auto-fit, minmax(min(100%, 276px), 1fr)); }
.native-source :deep(textarea) {font-family: ui-monospace, monospace;}
@media (max-width: 600px) {
  .catalog-tools { grid-template-columns: minmax(0, 1fr); }
  .record-actions { max-width: 7rem; justify-content: flex-end; }
  .runtime-table :deep(table) { min-width: 31rem; }
}
</style>
