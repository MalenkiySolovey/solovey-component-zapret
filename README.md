# Solovey Zapret Component

This repository owns the independent Solovey Zapret component foundation. It
keeps the user-facing binding small and typed:

```text
Routing -> Direct Outbound -> ZapretBinding -> Strategy -> Network Planner
```

The component does not own Routing, DNS, Outbounds, nftables, fw4, queue/mark
allocation or a privileged shell. `ZapretBinding` is valid only for a Direct
Outbound with an empty detour. `Enabled=false` projects no packet-path intent;
`Enabled=true` with a missing Strategy, missing component capability or an
unsupported Direct fails closed.

The current source slice contains one ordered native profile model and a
deterministic `nfqws2` command-line compiler. ComponentHost lifecycle start is
intentionally non-activating: an admitted Network Planner configuration must
be passed to a qualified activation adapter before any runtime handle can exist.
No scheduler,
process, queue, listener or packet capture is created by construction or by a
disabled/absent component.

The upstream engine is an external, checksum-verified build input recorded in
`upstream/zapret2.json`. It is not a fork and is not downloaded at runtime.

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

