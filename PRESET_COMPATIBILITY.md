# Preset and strategy compatibility

Reference frozen on 2026-11-25: official `zapretdiscordyoutube/zapretgui`, tag
`22.0.0.0`, commit `4934082a4069e2e66e35668cf6a03cf4c8de16d2`. The tag's
`docs/LICENSE` gives the MIT grant, copyright 2025-2026 censorliber. The pinned
native engine is upstream `bol-van/zapret2 v1.0.7`, commit
`038c97e5819df10f8e8c22a2f1fffe57f2f7379b`, MIT. These are distinct sources.

At the frozen GUI tag, `src/presets/builtin/winws2` has 136 text presets;
the largest is 50,093 bytes. The inventory used the tagged trees under
`src/presets`, `src/profile`, `src/lists`, `src/fakes`, `src/blockcheck`,
`src/ui` and `src/app`, including the portable archive and editor flows.
The public templates identify the individual reference Git blobs. TLS split
and Web TCP/QUIC are locally authored adaptations of a representative profile
pattern, not translations of an entire Windows configuration. The Windows
example is review-only. No GUI code, Lua, archives, icons, list data, or
binary assets were copied from the reference.

| Frozen feature family | Disposition and owner |
| --- | --- |
| Preset catalog, grouping, clone, ordering, raw review | Adopted into the Component's existing Strategy catalog and shared UI. A template has no runtime identity and imports always create a new record. |
| `--new`, `--name`, positive `--filter-tcp/udp`, single or dual `--filter-l3` | Admitted with stable managed IDs, original profile order and finite, explicit transport scope. A combined TCP/UDP filter in one profile, wildcard, negative, or over-budget port scope is not silently split or widened. |
| `--filter-l7`, `--payload`, `--in-range`, `--out-range` and `--lua-desync` | Admitted only for the typed TLS/HTTP/QUIC payload and action families already checked by the compiler: split, ordered HTTP, bounded QUIC and Circular with its qualified return path. Payload/range state is order-sensitive. Other values, matches, functions, parameter sets, or action chains stay inert with a reason. |
| `--hostlist-domains`, `--ipset-ip` and corresponding exclusions | Admitted as typed include/exclude filters. File-backed `--hostlist`/`--ipset` and exclusions can map one managed list per slot from `lists/<id>.txt` or existing matching content. No external files, automatic lists or wildcard path resolution. |
| `--blob`, fake assets, native named data | Inline hex or packaged `blobs/<id>.hex` can map to a managed QUIC asset only when consumed by an admitted action. Matching IDs are reused; different content is a conflict. A custom blob still requires the existing installed Host permission. This is not a Windows fake binary importer. |
| `--skip`, native `--template`/`--import`, unknown profile modifiers | Preview only. The typed runtime does not represent their effects; importing a file does not grant permission to expand or bypass profile scope. |
| `--wf-*`, `--writable`, platform migration, driver controls | Windows/runtime owner, not a Linux strategy. Keep original source and show the exact flag. No driver, host filesystem or firewall settings are translated. |
| `--ctrack-*`, `--ipcache-*`, queue, marks, debug | Engine/Planner-owned settings, not Strategy fields. Import cannot override them. |
| `--lua-init`, external scripts or payload paths | Trust-restricted. Accepted built-ins use pinned runtime Lua; no uploaded Lua or executable input is run. |
| Built-in ZIP/user directories, remote URLs, refresh | Only Solovey's bounded declarative ZIP schema, JSON and native text are accepted. Remote fetch/update, upstream file layouts and live watched directories are not used. |
| BlockCheck, logs and browser fixes | Reuse the existing bounded outbound diagnostic and generation-scoped runtime evidence. No automatic strategy search, browser changes, arbitrary targets, or extra service. |
| Tray, desktop UI, registry, updater, DNS/hosts mutation, Telegram proxy, legacy and premium features | Not ported. Existing Solovey owners retain lifecycle, DNS, routing, diagnostics and updates. |

## Separate resource limits

| Resource | Limit and reason |
| --- | --- |
| Saved catalog (including unused lists and blobs) | 256 KiB serialized, shared with Product settings. Removed catalog-wide list/blob counts; inactive entries consume bytes, not runtime slots. |
| Import transfer | 256 KiB input and 256 KiB total uncompressed ZIP data, at most 128 regular entries with per-entry limits. Only declared text forms; no extraction. Namespace/size checks happen before entries are read. |
| Native review and authoring | 64 KiB for inert source (covers the frozen corpus), 8 KiB for admitted editable text. Preview can explain text that cannot be stored as an active definition. |
| Definition and packet scope | 8 KiB typed definition and up to 64 explicit ports per transport, matching the installed receiver and Host scope. Runtime argv and nft transaction size are separately bounded. |
| Lists | 32 entries per filter/list and 2 KiB aggregate resolved filter bytes per Strategy. Shared storage is independent of that active definition budget. |
| QUIC assets | Up to eight managed assets and 1,200 custom bytes per Strategy, with the existing Host authorization. Storage outside an active definition uses the catalog byte budget. |

`strategy.txt` packages may also carry `lists/<id>.txt` and
`blobs/<id>.hex`; `strategy.json` packages contain only that file. Exported
ZIP and JSON preserve the sealed record, raw text, and its dependencies.
Converted native source is shown alongside the original. Conversion only
replaces accepted file/declaration dependencies with managed references;
unresolved options prevent saving. Native text export without its dependencies
is rejected in favor of ZIP or JSON. No import implicitly assigns an outbound
or applies runtime configuration.
