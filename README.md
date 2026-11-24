# Solovey Zapret Component

This repository owns the independent Solovey Zapret component foundation. It
keeps the user-facing binding small and typed:

```text
Routing -> Egress Outbound -> ZapretBinding -> Strategy -> Network Planner
```

The component does not own Routing, DNS, Outbounds, nftables, fw4, queue/mark
allocation or a privileged shell. The installed Host and Product qualify the
actual egress socket. An active binding cannot be attached to a logical group
or an intermediate hop that delegates its socket to a detour. `Enabled=false`
projects no packet-path intent. An enabled binding with a missing Strategy or
an unavailable packet path fails closed.

The current source slice contains one ordered native profile model and a
deterministic `nfqws2` command-line compiler. ComponentHost lifecycle start is
intentionally non-activating: an admitted Network Planner configuration must
be passed to a qualified activation adapter before any runtime handle can exist.
No scheduler,
process, queue, listener or packet capture is created by construction or by a
disabled/absent component.

The upstream engine is an external, checksum-verified build input recorded in
`upstream/zapret2.json`. It is not a fork and is not downloaded at runtime.

Static profiles support explicit TCP or UDP ports and optional IP-family
filters. The bounded QUIC Initial action defaults to the upstream built-in
blob. A consumer still needs installed broker
and Network Planner admission for the requested packet scope. For TCP HTTP
requests, a profile can run the pinned `http_hostcase` and `http_domcase`
actions in the stored order. The optional `spell` on `http_hostcase` changes
only the casing of the four letters in `Host`. Native Circular
uses ordered variants of the pinned `zapret-auto.lua` action. The compiler
requires an already-admitted return path before producing Circular arguments;
the installed outgoing-only profile still rejects activation. Native options
and the visual editor both preserve variant order and the reply observation
window. Arbitrary Lua and external file references remain unsupported.
Static split, QUIC and ordered HTTP actions support native `--out-range`.
Use `a` (always), `x` (never), or a range such as `-d3`, `n2<n8`, or `b100-`.
Counters are packets (`n`), data packets (`d`), bytes (`b`), and, on TCP,
relative sequence start (`s`) or end (`p`). `-` includes the upper bound;
`<` excludes it. In native text a range affects subsequent actions until
overridden or a new profile starts. Visual editing stores it per action.
Ranges do not alter admitted ports or add return capture. Restricted ranges
do not count toward unconditional built-in QUIC DNS coverage.
Selected bindings with distinct Planner marks share one native argument list.
Its byte budget includes argument overhead and leaves room for the installed
broker's fixed launcher arguments and environment.

Native host and IP include/exclude fields accept inline values or one named
list of the same kind per field. The Strategy catalog stores the shared lists;
bound definitions are re-sealed with the resolved values on list change. A
referenced list cannot be deleted or silently replaced by an import. Transfer
includes only referenced lists and identifies the pinned QUIC blob when used.
The editor contribution uses the same catalog revision for list edits, preview
and export; it requires the matching Product catalog API. Visual and native
text edits preserve the references separately from the sealed list values.

Use DNS names, IP addresses or CIDR prefixes for lists. The same catalog can
store up to eight named QUIC decoys as hexadecimal bytes. Select one in a QUIC
Initial profile or reference it in native editor text as
`--lua-desync=fake:blob=asset_<id>:repeats=1`. A Strategy has a total custom
blob budget of 1200 bytes. References are revision-bound and transfer with
their data; raw options never read a file or name a runtime Lua variable.
Custom Lua and file references are not admitted. Treat blobs as public wire
data, not secrets. Custom blobs need explicit installed host approval and do
not satisfy protected QUIC DNS coverage. An unconditional built-in QUIC
profile still has to cover unmatched traffic; a passive or custom profile
cannot shadow it.

## Checks

Run with Go 1.26.10 or newer and `GOWORK=off`:

```text
go test -race -count=1 ./...
go vet ./...
```

The Linux source build runs with `bash scripts/build-nfqws2.sh <new-directory>`.
It requires GCC, Make, pkg-config, Lua 5.4, zlib, libcap, libnetfilter-queue,
libnfnetlink and libmnl development libraries. Producer CI compares two builds on its Linux
amd64 toolchain and checks compiler output against the real engine. Product
owns the separate namespace proof; no component code downloads or starts an
engine at runtime. Neither Windows nor a production Node role is enabled here.

