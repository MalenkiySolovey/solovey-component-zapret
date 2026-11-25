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

### Frozen corpus dispositions

The [per-preset index](frozen-zapretgui.tsv) records the pinned Git object,
byte size and dependency references for each of the 136 `winws2` presets.
It also accounts for the 171 `winws1` documents, which use the legacy engine
and are outside the pinned Zapret2 grammar. The index contains references,
not mirrored presets, scripts or payloads.

The exact 136 `winws2` launch documents are Windows-specific as shipped:
each has Windows capture options and references packaged lists, binary assets,
and additional Lua. This is not a finding that every profile in those
documents is Linux-incompatible. The overlapping source dependency inventory
contains 92 list paths, 61 binary paths and eight Lua paths. Their bytes live
in the distribution and are absent from the public source tree. The tagged
fake catalog loader also names a separately packaged SQLite resource.

The [stable release manifest](https://git2.zapret.moe/zapretdiscordyoutube/zapretgui/releases/tag/22.0.0.0)
has SHA-256 `864bafffc54c034502a6256dc0e4b4bc0c61c8cfa8a61fe41703b1a82557b144`.
Its Ed25519 signature verifies with `gui-2026a` from the frozen tag. All 161
referenced objects matched their signed sizes and SHA-256 digests, including
all 92 lists under `lists/base/`. The installed Dev supplied 156 matching
objects. Five mismatches were resolved from the publisher's content store
without running the installer or Lua. The signed `zapret-lib.lua`,
`zapret-antidpi.lua` and `zapret-auto.lua` match the pinned Zapret2 v1.0.7
libraries byte for byte. Solovey obtains those libraries from Zapret2.

The signature establishes release provenance for the five additional GUI Lua
files and the list and binary assets. The manifest provides no per-file
license metadata, and redistribution rights for these additional dependencies
remain unconfirmed. Two base lists exceed the 256 KiB Solovey catalog budget
individually. `ipset-all.txt` is 572,927 bytes and `russia-blacklist.txt` is
1,773,215 bytes. The frozen distribution adds no runnable originals to the
catalog under the current runtime and transfer contracts.

The running Windows Dev build inspected separately reported `21.1.7.313`
in both the UI and executable properties, SHA-256
`1a510d18c9df548bd943be6d4c69990d3060753bfbb988c62ed3eeba08ad07c0`.
The official matching version tag resolves to
`58bfe19a85eb5dc1685d6197fb5d897a41b4a8d1`; version matching is not a
binary build attestation. The operator's checkout was at
`ca23042c1c8d93416436078520b65fd3f2faab23` (2026-11-17). The live overview,
navigation and stopped engine were visible; the other workflows were
compared in source. No strategy, probe or network setting was activated.

| Frozen feature family | Disposition and owner |
| --- | --- |
| Preset catalog, grouping, clone, ordering, raw review | Adopted into the Component's existing Strategy catalog and shared UI. Importing an adapted template creates a new Strategy with the immutable template ID, reference version/commit and SHA-256 of the template text. Later edits retain that origin independently of the runtime revision. |
| `--new`, `--name`, positive `--filter-tcp/udp`, single or dual `--filter-l3` | Admitted with stable managed IDs, original profile order and finite, explicit transport scope. Combined TCP/UDP stays in one native profile and exposes separate TCP and UDP ports or inclusive ranges in the shared editor. Wide scopes stay compact from native parsing through Host intent. The family and list filters apply to the whole profile. Wildcards and negative port filters still lack a qualified positive admission scope; Circular retains its TCP-only return path. |
| `--filter-l7`, `--payload`, `--in-range`, `--out-range` and `--lua-desync` | A native-only passive profile can use independent L7 selectors from the pinned engine. Its standalone original `pass` action can retain a native payload selector and bounded outgoing range. Neither transforms packets, and an earlier matching passive profile can still preempt a later action. Transforming actions retain the typed TLS/HTTP/QUIC payload contracts: split, ordered HTTP, bounded QUIC and Circular with its qualified return path. Other action chains and incoming range variants stay inert; options are not stripped. |
| `--hostlist-domains`, `--ipset-ip` and corresponding exclusions | Admitted as typed include/exclude filters. File-backed `--hostlist`/`--ipset` and exclusions can map one managed list per slot from `lists/<id>.txt` or existing matching content. No external files, automatic lists or wildcard path resolution. |
| `--blob`, fake assets, native named data | Inline hex or packaged `blobs/<id>.hex` or `blobs/<id>.bin` can map to a managed QUIC asset only when consumed by an admitted action. Matching IDs are reused; different content is a conflict. A custom blob still requires the existing installed Host permission. This does not import the GUI's packaged fake catalog. |
| `--skip`, native `--template`/`--import`, unknown profile modifiers | Preview only. The typed runtime does not represent their effects; importing a file does not grant permission to expand or bypass profile scope. |
| `--wf-*`, `--writable`, platform migration, driver controls | Windows/runtime owner, not a Linux strategy. Keep original source and show the exact flag. No driver, host filesystem or firewall settings are translated. |
| `--ctrack-*`, `--ipcache-*`, queue, marks, debug | Engine/Planner-owned settings, not Strategy fields. Import cannot override them. |
| `--lua-init`, external scripts or payload paths | Trust-restricted. Accepted built-ins use pinned runtime Lua. Administrator-authored, ZapretGUI and other external Lua source can be kept separately as inert drafts. Drafts do not resolve as Strategy dependencies and are never passed to the engine. |
| Built-in ZIP/user directories, remote URLs, refresh | Solovey ZIP, JSON and native text, plus version 1 ZapretGUI portable ZIP for supported native text and complete standalone lists. No filesystem extraction or automatic apply. Remote fetch/update and live watched directories are not used. |
| BlockCheck, logs and browser fixes | Reuse the existing bounded outbound diagnostic and generation-scoped runtime evidence. No automatic strategy search, browser changes, arbitrary targets, or extra service. |
| Tray, desktop UI, registry, updater, DNS/hosts mutation, Telegram proxy, legacy and premium features | Not ported. Existing Solovey owners retain lifecycle, DNS, routing, diagnostics and updates. |

## Separate resource limits

| Resource | Limit and reason |
| --- | --- |
| Saved catalog (including unused lists, blobs and Lua drafts) | 256 KiB serialized, shared with Product settings. Removed catalog-wide list/blob counts; inactive entries consume bytes, not runtime slots. |
| Import transfer | 256 KiB input and 256 KiB total uncompressed ZIP data, at most 128 regular entries with per-entry limits. Only declared text forms and bounded opaque QUIC data; no extraction. Namespace/size checks happen before entries are read. |
| Native review and authoring | 64 KiB for inert source (covers the frozen corpus), 8 KiB for admitted editable text. Preview can explain text that cannot be stored as an active definition. |
| Definition and packet scope | 8 KiB typed definition. Scopes of up to 64 ports retain the legacy representation for stable revisions; larger positive scopes use disjoint inclusive intervals instead of a per-port array. Runtime argv and nft transaction size are separately bounded. A Product with an older Planner and broker does not admit the new interval representation. |
| Lists | No fixed entry count; native lists use the existing 2 KiB aggregate filter-byte budget per Strategy. An 80-entry scope fits and is covered by the pinned engine's dry-run test. The shared catalog also has its own serialized size bound. |
| QUIC assets | Up to eight managed assets and 1,200 custom bytes per Strategy, with the existing Host authorization. Storage outside an active definition uses the catalog byte budget. |

`strategy.txt` packages may also carry `lists/<id>.txt` and
`blobs/<id>.hex` or `blobs/<id>.bin`; `strategy.json` packages contain only that file. Exported
ZIP and JSON preserve the sealed record, raw text, and its dependencies.
Converted native source is shown alongside the original. Conversion only
replaces accepted file/declaration dependencies with managed references;
unresolved options prevent saving. Native text export without its dependencies
is rejected in favor of ZIP or JSON. No import implicitly assigns an outbound
or applies runtime configuration.

The ZapretGUI reader uses the frozen `src/presets/portable_archive.py` layout:
`manifest.json`, `preset.txt`, and exactly the declared `lists/` members. It
checks each list's declared kind, storage mode and original normalized text
SHA-256 within the existing Solovey transfer bounds. Quoted Windows-style
relative list references map to managed IDs, never to host files. A same-ID
different-content list is a conflict. An `overlay` contains user additions to
a separately installed base; neither the additions alone nor an unverified
local list with the same filename is a substitute. Preview requests the full
effective list. A package whose native text includes Windows interception or
unadmitted options remains review-only; the importer does not drop options.
The saved strategy and Solovey transfer retain the source archive's format,
version, claimed name and input SHA-256, separate from the runtime revision.
For complete, referenced data files, the operator can choose a different
managed ID from import review and preview again. The remap applies only to
the incoming Strategy and leaves existing assets and their consumers intact.
It cannot repair an overlay, substitute missing bytes or rewrite a sealed
Solovey JSON transfer.
The same managed list selectors are available on native-owned records without
rewriting their raw source. The existing validation applies to the joint text
and list selection, including profile IDs and conflicts with inline lists.

## Lua authoring and runtime

The pinned `zapret-lib.lua`, `zapret-antidpi.lua` and `zapret-auto.lua`
remain on their existing qualified paths. At the pinned engine commit,
`nfq2/nfqws.c` drops UID before initializing Lua; `nfq2/sec.c` retains
`CAP_NET_ADMIN` and `CAP_NET_RAW`. `nfq2/lua.c` opens the standard libraries
and supplies native packet APIs in that same process. A file's author or a
successful syntax check cannot isolate it from that authority. Unpinned
code cannot be loaded or evaluated by the current runtime.

The admin-only draft workflow supports create, edit, explicit UTF-8 source
import, download and delete. Store origin and (for ZapretGUI or third-party
source) a version, source or unknown-origin note. Import does not run or
fetch anything. Drafts share the existing catalog revision and backup data;
Strategy transfers do not carry them. No ZapretGUI library is bundled or
declared compatible on the basis of the GUI's repository license.
