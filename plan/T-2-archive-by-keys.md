# T-2 — Model top-level ARCHIVE_BY_KEYS (yabs/server/libs/static)

## Upstream mechanism

`build/ymake.core.conf` (source-root /home/pg/monorepo/4):

```
macro ARCHIVE(NAME="", DONTCOMPRESS?"-p":"", Files...) {
    .CMD=$ARCH_TOOL -q -x $DONTCOMPRESS ${suf=\:;input:Files} -o ${addincl;noauto;output:NAME} ...
}
macro ARCHIVE_BY_KEYS(NAME="", KEYS="", DONTCOMPRESS?"-p":"", Files...) {
    .CMD=$ARCH_TOOL -q -x $DONTCOMPRESS ${input:Files} -k $KEYS -o ${addincl;noauto;output:NAME} ...
}
```

ARCHIVE_BY_KEYS differs from ARCHIVE only in the command shape:
- members listed **plain** (`${input:Files}`), no per-member `:` empty-key suffix;
- a single `KEYS` positional (a colon-joined key list authored verbatim) passed via `-k $KEYS`.

Everything else is identical: same `$ARCH_TOOL -q -x [DONTCOMPRESS]` prefix, the same
`${addincl;noauto;output:NAME}` output side effect (the build-dir of the archive enters
the module's local + global include buckets, so the declaring module's own compiles and
its PEERDIR consumers get `-I$(B)/<moddir>`), same `kv p=AR pc=light-red`.

Verified against `sg7.json` (`ay dev dump grep --raw`) for
`$(B)/yabs/server/libs/static/static_data.inc`: command lists each SRCDIR-backed
resource plain, then `-k <colon-joined keys>`, then `-o <archive>`; inputs = archiver +
all resources; consumer `resources.cpp.o` carries `-I$(B)/yabs/server/libs/static`, lists
the archive output as its first input and every archived resource as a source-closure
leaf. Transitive PEERDIR consumer `…/socdem_type.h_serialized.cpp.o` gets the `-I` only
(global addincl), no resource inputs.

## Current state

`emit_archives.go` already models the keyed command shape (`a.Keys != nil` lists members
plain + emits `-k <join ":">`), the `${addincl;noauto;output}` side effect
(`applyArchiveAddIncl` over `d.archives`), and the producer registration + source-member
closure (`PropagateSourceMembers`). The only gap: `ARCHIVE_BY_KEYS` sits in
`acknowledgedMacros` (gen.go) as a no-op; no parser turns the macro into an `ArchiveEntry`.
Only the synthetic `LJ_21_ARCHIVE` path builds keyed entries today.

## Change

1. `modules.go`: add `case tokArchiveByKeys:` → new `applyArchiveByKeysStmt`, parsing
   `NAME <name> KEYS <keys> [DONTCOMPRESS] files...` into an `ArchiveEntry` with
   `Keys: []string{<keys>}` (single positional, already colon-joined upstream) and
   `PropagateSourceMembers: true` (direct SRCDIR-backed resources ride into the C++
   consumer's source closure, matching the reference). Members feed `d.archives`, so the
   existing `applyArchiveAddIncl` + `emitArchives` paths handle addincl, command, inputs,
   producer registration and closure with no further change.
2. `gen.go`: drop `"ARCHIVE_BY_KEYS"` from `acknowledgedMacros` — it is now typed-handled.

The `"NAME"`, `"KEYS"`, `"DONTCOMPRESS"` literals in the parser satisfy the
service-keyword audit (`recordHandledMacro`).

## Test (fails before, passes after)

`emit_archives_test.go`: a LIBRARY with `ARCHIVE_BY_KEYS(NAME data.inc KEYS k1:k2 a.txt
sub/b.txt)` and an `SRCS(use.cpp)` that `#include`s `data.inc`. Assert:
- archive node `$(B)/mod/data.inc`: members plain, `-k k1:k2`, `-o …/data.inc`, kv AR;
- members resolve SRCDIR-backed (`$(S)/mod/...`), no `:` suffix;
- consumer `use.cpp.o` carries `-I$(B)/mod` and lists both resources as input-closure leaves.

Before the change the macro is inert, so no `data.inc` node exists → test fails.

## Gate

`go test ./...` + `./dev/validate.py`. Expect sg2–sg6 byte-exact (no plain-ARCHIVE
behavior touched), sg7 parity up (static_data.inc pair appears; resources.cpp.o and the
socdem serialized compile pair stop reporting the missing archive / resource inputs / `-I`).
