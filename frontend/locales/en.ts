export default { zapretCatalog: {
  title: 'Zapret strategies', refresh: 'Refresh', import: 'Import', export: 'Export', create: 'New strategy',
  edit: 'Edit strategy', clone: 'Clone strategy', delete: 'Delete strategy', empty: 'No managed strategies',
  unavailable: 'The Zapret component is not ready.', references: 'Referenced by', bindings: 'Direct bindings', direct: 'Direct',
  strategy: 'Desired strategy', status: 'Applied status', diagnostic: 'Test HTTPS through this Direct',
  states: { off: 'Off', applied: 'Applied', pending: 'Desired differs from applied', blocked: 'Blocked: no verified live path' },
  name: 'Name', tlsPreset: 'TLS/TCP preset', httpPreset: 'HTTP/TCP preset', visual: 'Profiles', raw: 'Native options',
  profile: 'Profile {index}', profileID: 'Profile ID', ports: 'TCP ports (comma-separated)', payload: 'Payload', split: 'Split byte',
  passive: 'No transformation', moveUp: 'Move profile up', moveDown: 'Move profile down', removeProfile: 'Remove profile', addProfile: 'Add profile',
  assetScope: 'Only built-in Lua from the pinned engine is allowed. Custom Lua, hostlists and paths are not imported.',
  editAffects: 'Applying profile changes restarts the protected bindings for: {refs}',
  deleteConfirm: 'Delete “{name}”?', cancel: 'Cancel', save: 'Save', copyName: '{name} (copy)',
  testOK: 'HTTPS through {direct}: {delay} ms', importInvalid: 'Select a Zapret export JSON file up to 32 KiB.',
} }
