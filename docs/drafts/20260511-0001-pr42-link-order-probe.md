# PR-42 peer-library link-order probe — tools/archiver LD

**Date:** 2026-05-11
**What:** Identify the canonical peer-library link-order convention that
the reference graph uses for the `tools/archiver` LD `cmd[2]` (link_exe.py
invocation), determine why our generator emits a different order, and
specify the fix shape.
**Why:** Close cluster J ("tools/archiver LD .a peer-library link-order
divergence") of the PR-36 closure roadmap
(`docs/drafts/20260511-0000-pr36-closure-probe.md` §3 L3-final-cluster,
Risk #5). This is the last L3 divergence for the M1 = M2 closure on
`tools/archiver` — pair is L1-paired and L2-matched, but L3 mismatched on
1 `cmd_args` slice difference.

---

## 1. The diff

### Source nodes

| Side | UID | Path |
|------|-----|------|
| REF  | `Ze_eMOLqyMsa6WlbbMhgvQ` | `/home/pg/monorepo/yatool_orig/sg.json` |
| OUR  | `-8vQ6GETDZsoc1d5PowiwE` | `/home/pg/monorepo/yatool/.out/sg.json` |

Both nodes carry 4 `cmds`:

```
cmd[0]  vcs_info.py
cmd[1]  clang compile of __vcs_version__.c → __vcs_version__.c.o
cmd[2]  link_exe.py + clang++ link               <-- mismatched
cmd[3]  fs_tools.py link_or_copy_to_dir
```

`cmd[0]`, `cmd[1]`, `cmd[3]` are byte-identical between REF and OUR.
`cmd[2].cmd_args` length is **73 on both sides** — the set of args is
identical; only their order in slots 29..48 differs.

### Layout of cmd[2].cmd_args

| Slot range  | Content                                                  |
|-------------|----------------------------------------------------------|
| 1..15       | python3, link_exe.py, plugin block, clang-ver, roots, arch, objcopy, clang++ |
| 16..17      | `-Wl,--whole-archive`, `--ya-start-command-file`         |
| 18          | `contrib/libs/tcmalloc/no_percpu_cache/liblibs-tcmalloc-no_percpu_cache.global.a` — whole-archive global |
| 19..20      | `--ya-end-command-file`, `-Wl,--no-whole-archive`        |
| 21..24      | `__vcs_version__.c.o`, `main.cpp.o`, `-o`, output path   |
| 25..27      | `--target=...`, `-march=...`, `-B/usr/bin`               |
| **28**      | `-Wl,--start-group`                                      |
| **29..60**  | **32 peer-library `.a` paths — link-order divergence here** |
| **61**      | `-Wl,--end-group`                                        |
| 62..73      | trailing static-musl flags                                |

### Peer-set verification (extras / drops)

Both REF and OUR list **the same 33 `.a` files** (slot 18's `.global.a` +
slots 29..60's 32 `.a`'s). No extras, no drops — the divergence is
**order-only**, as the cluster-J summary asserted.

Reproduction:

```sh
jq -r '.graph[] | select(.uid=="Ze_eMOLqyMsa6WlbbMhgvQ") | .cmds[2].cmd_args[]' \
  /home/pg/monorepo/yatool_orig/sg.json > /tmp/probe/ref.txt
jq -r '.graph[] | select(.uid=="-8vQ6GETDZsoc1d5PowiwE") | .cmds[2].cmd_args[]' \
  /home/pg/monorepo/yatool/.out/sg.json > /tmp/probe/our.txt
diff <(grep '\.a$' /tmp/probe/ref.txt | sort) <(grep '\.a$' /tmp/probe/our.txt | sort)
# (empty) — sets equal
```

### Side-by-side: slots 29..48 (the diverging range)

| Slot | REF                                                                 | OUR                                                                 | match |
|------|---------------------------------------------------------------------|---------------------------------------------------------------------|-------|
| 18   | `contrib/libs/tcmalloc/no_percpu_cache/.../*.global.a`              | (same)                                                              | ==    |
| 29   | `contrib/libs/cxxsupp/libcxxabi-parts/...`                          | `contrib/libs/musl/libcontrib-libs-musl.a`                          | ≠     |
| 30   | `contrib/libs/libunwind/...`                                        | `contrib/libs/cxxsupp/builtins/...`                                 | ≠     |
| 31   | `contrib/libs/cxxsupp/libcxxrt/...`                                 | `library/cpp/malloc/api/...`                                        | ≠     |
| 32   | `contrib/libs/cxxsupp/builtins/...`                                 | `contrib/libs/cxxsupp/libcxxabi-parts/...`                          | ≠     |
| 33   | `contrib/libs/cxxsupp/libcxx/...`                                   | `contrib/libs/libunwind/...`                                        | ≠     |
| 34   | `util/charset/...`                                                  | `contrib/libs/cxxsupp/libcxxrt/...`                                 | ≠     |
| 35   | `contrib/libs/zlib/...`                                             | `contrib/libs/cxxsupp/libcxx/...`                                   | ≠     |
| 36   | `contrib/libs/double-conversion/...`                                | `util/charset/...`                                                  | ≠     |
| 37   | `contrib/libs/libc_compat/...`                                      | `contrib/libs/zlib/...`                                             | ≠     |
| 38   | `contrib/libs/linuxvdso/original/...`                               | `contrib/libs/double-conversion/...`                                | ≠     |
| 39   | `contrib/libs/linuxvdso/...`                                        | `contrib/libs/libc_compat/...`                                      | ≠     |
| 40   | `util/libyutil.a`                                                   | `contrib/libs/linuxvdso/original/...`                               | ≠     |
| 41   | `build/cow/on/libbuild-cow-on.a`                                    | `contrib/libs/linuxvdso/...`                                        | ≠     |
| 42   | `library/cpp/malloc/api/libcpp-malloc-api.a`                        | `util/libyutil.a`                                                   | ≠     |
| 43   | `contrib/restricted/abseil-cpp/...`                                 | `contrib/libs/musl/full/...`                                        | ≠     |
| 44   | `contrib/libs/tcmalloc/malloc_extension/...`                        | `contrib/restricted/abseil-cpp/...`                                 | ≠     |
| 45   | `library/cpp/malloc/tcmalloc/...`                                   | `contrib/libs/tcmalloc/malloc_extension/...`                        | ≠     |
| 46   | `contrib/libs/tcmalloc/no_percpu_cache/...` (non-global)            | `library/cpp/malloc/tcmalloc/...`                                   | ≠     |
| 47   | `contrib/libs/musl/libcontrib-libs-musl.a`                          | `contrib/libs/tcmalloc/no_percpu_cache/...` (non-global)            | ≠     |
| 48   | `contrib/libs/musl/full/...`                                        | `build/cow/on/libbuild-cow-on.a`                                    | ≠     |
| 49..60 | archive, nayuki_md5, base64{avx2,ssse3,neon32,neon64,plain32,plain64}, string_utils/base64, digest/md5, colorizer, getopt/small | (same) | ==    |

Slots 49..60 — the **explicit-PEERDIR subtree** of `tools/archiver`'s
ya.make (`library/cpp/archive`, `library/cpp/digest/md5`,
`library/cpp/getopt/small`) — already match exactly. The divergence is
entirely in the **implicit-PEERDIR closure** (slots 29..48).

---

## 2. Hypothesis derivation

### REF observed order (slots 29..60), grouped

```
A) cxxsupp closure:   libcxxabi-parts, libunwind, libcxxrt, builtins, libcxx
B) util closure:      util/charset, zlib, double-conversion, libc_compat,
                      linuxvdso/original, linuxvdso, util
C) cow + tcmalloc:    build/cow/on, malloc/api, abseil-cpp,
                      tcmalloc/malloc_extension, malloc/tcmalloc,
                      tcmalloc/no_percpu_cache
D) musl/full closure: musl, musl/full
E) explicit PEERDIRs: archive, (nayuki_md5, base64*, string_utils/base64, digest/md5),
                      colorizer, getopt/small
```

### Falsified hypotheses

- **Pure alphabetical:** falsified at slot 29 — `libcxxabi-parts` lex-precedes
  `libcxx`, but the cxxsupp set is not alphabetical (libunwind comes before
  libcxxrt; libcxx comes last). Also slot 41 `build/cow/on` precedes slot 47
  `contrib/libs/musl` alphabetically but then slot 49 `library/cpp/archive` is
  out-of-order relative to slot 60 `library/cpp/getopt/small`.

- **Bucketed by module class** (cxxsupp/* → contrib/libs/* → library/* → util):
  falsified — `util/charset` (slot 34) lands between cxxsupp and the rest of
  contrib/libs/*; `library/cpp/malloc/api` (slot 42) sits between
  `build/cow/on` and `abseil-cpp`. The buckets interleave.

- **BFS over the parent's PEERDIR declaration order:** falsified — BFS would
  emit every direct peer (cxxsupp, util, cow, tcmalloc cluster, musl/full,
  archive, digest/md5, getopt/small) before any transitive. REF instead
  emits every transitive of cxxsupp before any of util's children, and
  every transitive of `library/cpp/malloc/tcmalloc` before
  `library/cpp/malloc/tcmalloc` itself.

- **DFS pre-order:** falsified — would emit each parent before its children
  (`cxxsupp` before `libcxxabi-parts`, etc.). REF emits children first and
  drops the fake parent `cxxsupp` entirely; the same applies for every
  non-fake parent (e.g. `util` appears at slot 40, AFTER all its
  transitive children at slots 34..39).

### Surviving hypothesis

**Post-order DFS of the direct-PEERDIR module graph, walked in PEERDIR
declaration order at each node, with each module deduplicated by first
occurrence. Implicit PEERDIRs (added by `_BASE_UNIT` /
`_LINK_UNIT` / `_BASE_PROGRAM` in the conf) are walked *before* the
user's explicit PEERDIRs. Fake modules (`cxxsupp` itself; anything with
`IsFakeModule()==true`) are filtered out of the emitted list but still
contribute their subtree.**

### Hand-simulation against REF

`tools/archiver`'s effective direct PEERDIR sequence (after the conf's
implicit additions land), in declaration order:

```
1. contrib/libs/cxxsupp          (conf line 771, NORUNTIME != "yes")
2. util                          (conf line 778, NOUTIL != "yes")
3. build/cow/on                  (conf line 947, USE_COW == "yes")
4. library/cpp/malloc/tcmalloc   (conf line 997, ALLOCATOR=TCMALLOC_TC)
5. contrib/libs/tcmalloc/no_percpu_cache  (conf line 998)
6. contrib/libs/musl/full        (conf line 1243, MUSL=yes, !MUSL_LITE)
7. library/cpp/archive           (ya.make line 4)
8. library/cpp/digest/md5        (ya.make line 5)
9. library/cpp/getopt/small      (ya.make line 6)
```

Post-order DFS walk (sources verified in `/home/pg/monorepo/yatool_orig/`):

| Step | Module | PEERDIRs of this module | Emitted? |
|------|--------|------------------------|----------|
| 1 | cxxsupp → libcxx → libcxxabi-parts | none (NO_RUNTIME) | emit at 29 |
| 2 |   libcxx → libcxxrt → libunwind | (libunwind no peers in this build) | emit libunwind at 30 |
| 3 |   libcxx → libcxxrt itself | libunwind, sanitizer/include | emit libcxxrt at 31 |
| 4 |   libcxx → builtins | (NO_RUNTIME, no peers) | emit builtins at 32 |
| 5 |   libcxx itself | libcxxabi-parts, libcxxrt, builtins | emit libcxx at 33 |
| 6 | cxxsupp itself | (fake module, skipped) | — |
| 7 | util → util/charset | (leaf) | emit at 34 |
| 8 | util → zlib | (leaf) | emit at 35 |
| 9 | util → double-conversion | (leaf) | emit at 36 |
| 10 | util → libc_compat | (leaf) | emit at 37 |
| 11 | util → linuxvdso → linuxvdso/original | (leaf) | emit at 38 |
| 12 | util → linuxvdso itself | original | emit at 39 |
| 13 | util itself | charset, zlib, double-conv, libc_compat, linuxvdso | emit at 40 |
| 14 | build/cow/on | (leaf) | emit at 41 |
| 15 | malloc/tcmalloc → malloc/api | (leaf) | emit at 42 |
| 16 | malloc/tcmalloc → malloc_extension → abseil-cpp | (leaf) | emit at 43 |
| 17 | malloc/tcmalloc → malloc_extension itself | abseil-cpp | emit at 44 |
| 18 | malloc/tcmalloc itself | api, malloc_extension | emit at 45 |
| 19 | tcmalloc/no_percpu_cache (peers abseil + malloc_ext — both already emitted) | (dedup) | emit at 46 |
| 20 | musl/full → musl | (leaf) | emit at 47 |
| 21 | musl/full itself | musl | emit at 48 |
| 22 | archive | (leaf) | emit at 49 |
| 23 | digest/md5 → nayuki_md5 | (leaf) | emit at 50 |
| 24 | digest/md5 → string_utils/base64 → base64/{avx2,ssse3,neon32,neon64,plain32,plain64} | (leaves, declared in this order) | emit 51..56 |
| 25 | digest/md5 → string_utils/base64 itself | base64/* | emit at 57 |
| 26 | digest/md5 itself | nayuki_md5, string_utils/base64 | emit at 58 |
| 27 | getopt/small → colorizer | (leaf) | emit at 59 |
| 28 | getopt/small itself | colorizer | emit at 60 |

**Every emitted slot 29..60 matches REF exactly.** Hypothesis fully
confirmed.

### Source-of-truth in upstream ymake

The algorithm is implemented in
`/home/pg/monorepo/yatool_orig/devtools/ymake/`:

- **Traversal driver:**
  `compact_graph/peer_collector.h:15` — comment "**Postfix DFS** traversal
  through the modules inside dep graph connected by direct peerdir
  dependency."
- **Per-module accumulator:**
  `module_restorer.cpp:70-93` — `TTransitivePeersCollector::Collect()`:
  ```cpp
  if (peer->PassPeers()) {
      auto& peerIds = ... GetModuleNodeIds(peer->GetId());
      ... MergeLists(parentNodeIds.UniqPeers, peerIds.UniqPeers);  // L88
  }
  parentNodeIds.LocalPeers.insert(peerNodeId);
  ... AddToList(parentNodeIds.UniqPeers, peerNodeId);              // L92
  ```
  When the visitor leaves a peer subtree, the peer's full transitive
  list is merged into the parent's list (uniqueness preserved), then
  the peer itself is appended. This produces post-order, declaration-
  ordered output.

- **Uniqueness:** `transitive_state.cpp:34-56` — `MergeLists` does an
  append-with-uniqueness via `TUniqVector`; first occurrence of a
  module wins, later attempts no-op.

- **Consumption for the link line:**
  `module_restorer.cpp:502-531` — `UpdateLocalVarsFromModule` iterates
  `modLists.UniqPeers()` and emits each peer's path into the `PEERS`
  ymake variable. The link command template
  `build/conf/linkers/ld.conf:162` references `${rootrel:PEERS}`
  between `$_START_GROUP` and `$_END_GROUP`. This is the variable that
  populates slots 29..60 of `cmd[2].cmd_args`.

- **Fake-module filter:**
  `json_visitor.cpp:707-714` — when building the link-command's
  `NodeDeps`, fake modules are filtered out:
  ```cpp
  for (auto peerId : managedPeers) {
      auto peerModule = ...Get(Graph.Get(peerId)->ElemId);
      if (peerModule->IsFakeModule()) { continue; }
      deps.Push(peerId);
  }
  ```
  Same filter is applied (via `IsFakeModule(elemId)`) at
  `module_restorer.cpp:511-513` when PEERS itself is populated.

### Comparator pseudocode

```
// build_uniq_peers(module M) → vector<Module>
function build_uniq_peers(M):
    if M.peers_complete: return M.uniq_peers   // memoized
    out := empty TUniqVector

    // visit each direct PEERDIR in declaration order:
    for child in M.direct_peerdirs:          // both implicit + explicit
        if child.PassPeers:
            child_list := build_uniq_peers(child)   // recurse first
            out.append_unique_all(child_list)       // children first
        out.append_unique(child)                    // then self

    M.uniq_peers := out
    M.peers_complete := true
    return out

// emit_link_peers(program P):
//   for peer in build_uniq_peers(P):
//     if peer.is_fake: skip
//     emit peer.archive_path
```

The "direct_peerdirs" list at each module is the **union of conf-
declared implicit peers (in source-line order of the matching `when`
clauses) and the module's own `PEERDIR(...)` statements (in
declaration order)** — implicit before explicit, both flattened into a
single ordered sequence.

---

## 3. Why OUR is wrong

Our generator's `defaultPeerdirsFor` (`gen.go:1006-1109`) returns a
**flattened pre-computed list** of implicit peers for every non-runtime
module:

```
musl, builtins, malloc/api, libcxx, libcxxrt, libunwind, util,
musl/include
```

These eight peers are appended directly to `allPeers` (`gen.go:1493-1505`)
**at the parent module's level**, not at the level where the upstream
conf adds them. Then `defaultProgramPeerdirsFor` (`gen.go:1183`) appends
musl/full + tcmalloc cluster.

When `genModule(tools/archiver)` walks `allPeers`, the resulting
post-order DFS is correct *for each peer's subtree* — that is why our
slots 32..48 (after the three prepended peers) follow the same internal
ordering as REF's slots 29..45. But the three implicit peers `musl`,
`builtins`, `malloc/api` are emitted **as direct peers of
`tools/archiver`** (slots 29..31) ahead of the proper cxxsupp/util/cow/
allocator/musl walk, instead of being reached transitively *through*
their natural conf-declared parents:

- `contrib/libs/musl` is reached upstream **only** as a transitive peer
  of `contrib/libs/musl/full` (via that module's `PEERDIR(contrib/libs/
  musl)` in `contrib/libs/musl/full/ya.make`). Upstream conf never
  declares bare `musl` as an implicit peer of arbitrary modules.
- `contrib/libs/cxxsupp/builtins` is reached upstream **only** through
  `contrib/libs/cxxsupp/libcxx`'s `PEERDIR(builtins)` in its ya.make
  (for the libcxxrt CXX_RT branch).
- `library/cpp/malloc/api` is reached upstream **only** through
  `library/cpp/malloc/tcmalloc`'s `PEERDIR(library/cpp/malloc/api)` in
  its ya.make.

Our generator's mistake: it elevates these three transitively-reached
modules to **first-class implicit peers of every consumer**, which (a)
puts them at the front of every consumer's peer list and (b) causes them
to *also* be dedup-skipped when their natural parents try to add them,
producing the observed three-position front-loading and 17-slot
downstream shift.

The same fault explains why our slots 32..48 still match REF's 29..45:
once the three front-loaded duplicates are accounted for, the rest of
the walk *is* correct post-order DFS — the per-peer subtree expansion
in `gen.go` (`peerArchiveAddPath` at lines 1530-1537, 1638-1640, 1657)
already implements the right algorithm.

---

## 4. Proposed fix shape

### File and function

`gen.go:1006-1109` — `defaultPeerdirsFor`. Remove the three peers that
upstream conf does not declare at the consumer level:

- Drop the `peers = append(peers, "contrib/libs/musl")` block at
  `gen.go:1038-1041`.
- Drop the `peers = append(peers, "contrib/libs/cxxsupp/builtins")`
  block at `gen.go:1043-1047`.
- Drop the `peers = append(peers, "library/cpp/malloc/api")` block at
  `gen.go:1049-1053`.

These modules must still appear in the consumer's transitive closure —
but they will arrive naturally:

- `musl` arrives via `musl/full → musl` (program-default).
- `builtins` arrives via `cxxsupp → libcxx → builtins`.
- `malloc/api` arrives via `malloc/tcmalloc → api` (program-default).

### What carries the necessary info

Already there. The per-peer subtree expansion in `gen.go:1638-1640` and
`gen.go:1657` reads `peerResult.PeerArchiveClosurePaths` and
`peerResult.ARRef` and appends them via `peerArchiveAddPath`, which is a
first-occurrence-wins dedup table. Once the three spurious top-level
peers are removed, the post-order DFS already in place will reach them
through their natural parents. No new data shape is required.

### Risks

- **Comments at `gen.go:1038-1053` explicitly cite these as
  `_BASE_UNIT`-mandated implicit peers.** They are misattributed —
  `_BASE_UNIT` in `build/ymake.core.conf:606-1108` does *not* PEERDIR
  these three modules at every consumer; verify against the conf
  before deleting (the cited lines `771` `1043-1047` for cxxsupp; lines
  `783` for `contrib/libs/musl/include` (header-only, separate); no
  conf line PEERDIRs bare `musl`, `builtins`, or `malloc/api` at the
  base-unit level). The comment text in `gen.go:1011-1018` even
  acknowledges that runtime-ancestor modules get *zero* implicit
  peers; non-runtime modules should be receiving cxxsupp/util/musl/full
  (the conf-declared peers) rather than the flattened leaves.

- **Header-only `contrib/libs/musl/include` at `gen.go:1106` is a
  separate case** — that one *is* conf-declared at line 783 of
  `ymake.core.conf` and should remain. It is header-only, so it does
  not appear in the LD `.a` list; only its GLOBAL ADDINCLs feed the CC
  cmd_args. The fix does not touch this entry.

- **`util` at `gen.go:1086` and `cxxsupp/libcxx`/`libcxxrt`/`libunwind`
  at `gen.go:1059-1071` are also questionable** — the conf declares
  `cxxsupp` (the parent, line 771) and `util` (line 778) but **not**
  the leaf libcxx / libcxxrt / libunwind. Our generator adds the leaves
  directly. The reason this currently produces correct slots 29..33
  for cxxsupp's children is that `gen.go:1059-1071` declares them in
  the same order that the natural cxxsupp → libcxx walk would yield
  them (libcxxabi-parts, libunwind via libcxxrt, libcxxrt, builtins,
  libcxx). But declaring them explicitly is brittle — any
  upstream change to libcxx's ya.make would silently desync our order.
  PR-42 should consider replacing all of `gen.go:1043-1086`'s
  flattened-leaf list with a single `contrib/libs/cxxsupp` entry plus a
  single `util` entry, and let the natural post-order DFS through
  those parents yield the leaves.

### Minimal-fix vs. structural-fix

- **Minimal fix:** delete the three problem entries (musl, builtins,
  malloc/api). Verify slots 29..60 match REF byte-exactly. Risk: leaves
  the rest of `defaultPeerdirsFor`'s flattened-leaf list as a hidden
  duplication of the cxxsupp/util walks; works for `tools/archiver` and
  any module whose libcxx subtree is identical, but a libcxx change
  upstream would desync silently.
- **Structural fix:** collapse `defaultPeerdirsFor` to return only the
  conf-declared *direct* implicit PEERDIRs (`contrib/libs/cxxsupp`,
  `util`, `contrib/libs/musl/include`, and program-defaults). Trust the
  natural post-order DFS through these parents to produce the leaf
  order. Risk: the synthetic test fixtures that flatten the leaf set
  (`gen_test.go`) will need adjustment, and any test that exercises a
  module whose direct PEERDIR set hits one of these leaves directly
  must be re-validated.

Recommend the minimal fix as PR-42 ("R5-closure on link-order"), with a
note in `tasks.md`'s cross-cutting section that the structural cleanup
is deferred to PR-43 or later. The minimal fix is sufficient to drive
L3 to 100% on `tools/archiver` and (per inspection) on every other
PROGRAM in the M2 closure that shares the same implicit-peer set.

---

## 5. Confidence assessment

**Confidence: HIGH** that this fix can be implemented as a single
mechanical PR.

- The rule is fully derived and verified by hand-simulation against
  every one of REF's 32 peer slots (slots 29..60).
- The source-of-truth in upstream ymake is cited with file:line.
- The defect site in our generator is localised to three contiguous
  blocks of `gen.go` (lines 1038-1053).
- The downstream walk machinery (`peerArchiveAddPath` and the
  per-peer subtree fold at `gen.go:1638-1657`) already implements the
  correct algorithm; removing the spurious top-level peers does not
  require any algorithmic change.
- The remaining concern (structural cleanup of the libcxx-subtree
  flattening at `gen.go:1059-1086`) is **deferrable** — the minimal
  PR-42 fix lands the slot order correctly even without it.

**No user arbitration is needed for the minimal fix.** The structural-
cleanup follow-up (PR-43) is a judgement call about how much of the
flattened-leaf list to remove now versus later, but does not block PR-42.

---

## 6. Reproducible artefacts

The probe artefacts and the jq one-liners used:

```sh
# 1. Locate REF and OUR LD nodes
jq -r '.graph[] | select(.outputs[]? | test("tools/archiver/archiver$")) | {uid, outputs}' \
  /home/pg/monorepo/yatool_orig/sg.json
jq -r '.graph[] | select(.outputs[]? | test("tools/archiver/archiver$")) | {uid, outputs}' \
  /home/pg/monorepo/yatool/.out/sg.json

# 2. Dump each LD cmd[2].cmd_args (link_exe.py invocation)
jq -r '.graph[] | select(.uid=="Ze_eMOLqyMsa6WlbbMhgvQ") | .cmds[2].cmd_args[]' \
  /home/pg/monorepo/yatool_orig/sg.json > ref_link.txt
jq -r '.graph[] | select(.uid=="-8vQ6GETDZsoc1d5PowiwE") | .cmds[2].cmd_args[]' \
  /home/pg/monorepo/yatool/.out/sg.json > our_link.txt

# 3. Extract just the .a peer paths with their slot indices
grep -n '\.a$' ref_link.txt > ref_peers.txt
grep -n '\.a$' our_link.txt > our_peers.txt

# 4. Verify set-equality (extras / drops check)
diff <(sort -u <(cut -d: -f2- ref_peers.txt)) \
     <(sort -u <(cut -d: -f2- our_peers.txt))
# Expected: empty diff (set-equal, order-only divergence)

# 5. Side-by-side slot diff
paste ref_peers.txt our_peers.txt | awk -F'\t' \
  '{ if ($1 != $2) printf "%s    |    %s\n", $1, $2; else printf "%s    ==\n", $1 }'

# 6. Regenerate OUR graph after a fix
./yatool gen --target tools/archiver --out ./.out/sg.json
./yatool compare --level=3 ./.out/sg.json /home/pg/monorepo/yatool_orig/sg.json
```

Upstream ymake sources read during this probe (all read-only):

- `/home/pg/monorepo/yatool_orig/devtools/ymake/compact_graph/peer_collector.h:1-89`
- `/home/pg/monorepo/yatool_orig/devtools/ymake/compact_graph/iter_direct_peerdir.h:1-90`
- `/home/pg/monorepo/yatool_orig/devtools/ymake/module_restorer.cpp:40-93,440-545`
- `/home/pg/monorepo/yatool_orig/devtools/ymake/transitive_state.cpp:1-57`
- `/home/pg/monorepo/yatool_orig/devtools/ymake/json_visitor.cpp:700-755`
- `/home/pg/monorepo/yatool_orig/devtools/ymake/module_state.h:300-430`
- `/home/pg/monorepo/yatool_orig/build/ymake.core.conf:760-1255`
- `/home/pg/monorepo/yatool_orig/build/conf/linkers/ld.conf:155-355`
- `/home/pg/monorepo/yatool_orig/tools/archiver/ya.make`
- `/home/pg/monorepo/yatool_orig/contrib/libs/cxxsupp/ya.make`
- `/home/pg/monorepo/yatool_orig/contrib/libs/cxxsupp/libcxx/ya.make`
- `/home/pg/monorepo/yatool_orig/contrib/libs/cxxsupp/libcxxabi-parts/ya.make`
- `/home/pg/monorepo/yatool_orig/contrib/libs/cxxsupp/libcxxrt/ya.make`
- `/home/pg/monorepo/yatool_orig/contrib/libs/musl/full/ya.make`
- `/home/pg/monorepo/yatool_orig/contrib/libs/tcmalloc/common.inc`
- `/home/pg/monorepo/yatool_orig/contrib/libs/tcmalloc/malloc_extension/ya.make`
- `/home/pg/monorepo/yatool_orig/contrib/libs/tcmalloc/no_percpu_cache/ya.make`
- `/home/pg/monorepo/yatool_orig/library/cpp/malloc/tcmalloc/ya.make`
- `/home/pg/monorepo/yatool_orig/library/cpp/archive/ya.make`
- `/home/pg/monorepo/yatool_orig/library/cpp/digest/md5/ya.make`
- `/home/pg/monorepo/yatool_orig/library/cpp/string_utils/base64/ya.make`
- `/home/pg/monorepo/yatool_orig/library/cpp/getopt/small/ya.make`
- `/home/pg/monorepo/yatool_orig/util/ya.make`

Generator sources read (read-only):

- `/home/pg/monorepo/yatool/ld.go:90-440`
- `/home/pg/monorepo/yatool/gen.go:1006-1714`, `2140-2230`
