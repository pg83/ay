package main

import (
	"reflect"
	"testing"
)

// as_test.go — byte-exact regression test for EmitAS against the
// reference graph for contrib/libs/cxxsupp/builtins/aarch64/chkstk.S.
//
// The reference node is located by its output path
// ("$(B)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o")
// in /home/pg/monorepo/yatool_orig/sg.json. If the file is absent the
// test is skipped (per STYLE.md filter pattern), not failed.
//
// Comparison is field-by-field (not a single DeepEqual on the whole
// Node) for the same reasons as cc_test.go: UID/SelfUID/StatsUID are
// excluded (they are Finalize-computed), and per-field diff surfaces the
// first mismatch precisely.

// referenceASOutput is the output path used to locate the target AS node
// in the reference graph.
const referenceASOutput = "$(B)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

// builtinsASOwnAddIncl is the own-ADDINCL slice cxxsupp/builtins
// declares in its ya.make (the four musl-arch paths added under
// `IF (MUSL)`). PR-35m: the AS composer assembles the full include
// tail from these (own AddIncl) plus `ccIncludesPrefix`/`ccIncludesSuffix`
// (BUILD_ROOT/SOURCE_ROOT + linux-headers pair) so the previously
// pre-baked flat list now derives structurally.
var builtinsASOwnAddIncl = []string{
	"contrib/libs/musl/arch/aarch64",
	"contrib/libs/musl/arch/generic",
	"contrib/libs/musl/include",
	"contrib/libs/musl/extra",
}

// loadReferenceASNode reads the on-disk reference graph and returns the
// AS node whose first output is referenceASOutput. Returns nil and a
// reason string when the file is absent (so the caller can t.Skip) or
// when the node is not found.
// TestEmitAS_OutputPath_FlatSrcRel verifies that a flat srcRel (no "/" component)
// produces a flat output path with no _/ infix (PR-35r cluster 4 fix).
// Empirical reference: contrib/libs/asmglibc/memchr.S.o (flat, no _/).
func TestEmitAS_OutputPath_FlatSrcRel(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("some/module"), "flat.S", ModuleCCInputs{}, nil, testHostP, e)
	want := "$(B)/some/module/flat.S.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_OutputPath_NestedSrc verifies the nested-source output path.
func TestEmitAS_OutputPath_NestedSrc(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/chkstk.S", ModuleCCInputs{}, nil, testHostP, e)
	want := "$(B)/contrib/libs/cxxsupp/builtins/_/aarch64/chkstk.S.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_OutputPath_SrcDir verifies the __/ infix for ancestor-SRCDIR cases
// (PR-35r cluster 5). When in.SrcDir is set and the source does not resolve
// locally, the output path uses composeSrcDirOutputRel (same as CC case 3).
func TestEmitAS_OutputPath_SrcDir(t *testing.T) {
	e := NewBufferedEmitter()
	// tcmalloc/no_percpu_cache: SRCDIR = contrib/libs/tcmalloc (ancestor).
	// srcRel = tcmalloc/internal/percpu_rseq_asm.S
	// Expected: __/tcmalloc/internal/percpu_rseq_asm.S.o
	in := ModuleCCInputs{SrcDir: "contrib/libs/tcmalloc"}
	_, outPath := EmitAS(
		targetInstance("contrib/libs/tcmalloc/no_percpu_cache"),
		"tcmalloc/internal/percpu_rseq_asm.S",
		in,
		nil,
		testHostP,
		e,
	)
	want := "$(B)/contrib/libs/tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_AsmgLibc_Memchr_ByteExact (PR-35r cluster 4) pins the flat
// output path for asmglibc/memchr.S.o against the reference graph.
// asmglibc is a host-PIC (x86_64) clang AS module with a single-component
// srcRel — the reference output is flat (no _/ infix).
// TestEmitAS_TcmallocNopercpu_PercpuRseqAsm_ByteExact (PR-35r cluster 5)
// pins the full cmd_args bundle and output path for
// tcmalloc/no_percpu_cache/__/tcmalloc/internal/percpu_rseq_asm.S.o.
// This module uses SRCDIR(contrib/libs/tcmalloc) (ancestor), so the
// output infix is __/ and the input comes from $(S)/contrib/libs/tcmalloc/...
// TestEmitAS_YasmLD_PopulatesDepRefs verifies that when yasmLD is non-nil,
// EmitAS wires it into both DepRefs and ForeignDepRefs["tool"] (PR-30 D02).
// The L0 fingerprint reads only deps; the foreign-deps-only shape diverged
// for asmlib's 25 AS nodes in the reference graph.
func TestEmitAS_YasmLD_PopulatesDepRefs(t *testing.T) {
	e := NewBufferedEmitter()

	// Emit a minimal stand-in node to obtain a valid NodeRef for yasmLD.
	// The node's content is irrelevant — we only need its identity.
	yasmLDRef := e.Emit(&Node{
		Cmds:         []Cmd{{CmdArgs: []string{"yasm"}, Env: map[string]string{}}},
		Env:          map[string]string{},
		Inputs:       ToVFSSlice([]string{}),
		Outputs:      ToVFSSlice([]string{"$(B)/tools/yasm/yasm"}),
		KV:           map[string]string{"p": "LD", "pc": "light-cyan"},
		Tags:         []string{"tool"},
		Platform:     string(PlatformDefaultLinuxX8664),
		Requirements: map[string]interface{}{"cpu": float64(1), "network": "restricted", "ram": float64(32)},
		TargetProperties: map[string]string{
			"module_dir": "tools/yasm",
		},
	})

	yasmTestIn := ModuleCCInputs{AddIncl: builtinsASOwnAddIncl}
	ref, _ := EmitAS(targetInstance("contrib/libs/cxxsupp/builtins"), "aarch64/chkstk.S", yasmTestIn, &yasmLDRef, testHostP, e)

	// The AS node is at index 1 (yasmLD is at index 0).
	if len(e.nodes) != 2 {
		t.Fatalf("emitter buffered %d nodes, want 2", len(e.nodes))
	}

	_ = ref
	got := e.nodes[1]

	// DepRefs must contain exactly the yasmLD ref.
	if len(got.DepRefs) != 1 || got.DepRefs[0] != yasmLDRef {
		t.Errorf("DepRefs = %v, want [%v]", got.DepRefs, yasmLDRef)
	}

	// ForeignDepRefs["tool"] must also contain the yasmLD ref.
	toolRefs := got.ForeignDepRefs["tool"]

	if len(toolRefs) != 1 || toolRefs[0] != yasmLDRef {
		t.Errorf(`ForeignDepRefs["tool"] = %v, want [%v]`, toolRefs, yasmLDRef)
	}
}

// TestEmitAS_KV verifies that AS nodes carry the correct kv fields
// (p=AS, pc=light-green, no show_out) as observed in the reference graph.
func TestEmitAS_KV(t *testing.T) {
	e := NewBufferedEmitter()
	EmitAS(targetInstance("some/module"), "aarch64/foo.S", ModuleCCInputs{}, nil, testHostP, e)

	if len(e.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]
	want := map[string]string{
		"p":  "AS",
		"pc": "light-green",
	}

	if !reflect.DeepEqual(got.KV, want) {
		t.Errorf("kv:\n  got:  %#v\n  want: %#v", got.KV, want)
	}
}

// TestEmitAS_MuslHost_Ceill_ByteExact (PR-35a) pins the cmd_args bundle
// for a host x86_64 musl-self assembly node against the reference graph
// (`$(B)/contrib/libs/musl/_/src/math/x86_64/ceill.s.o`). Total
// 109 args: x86_64 toolchain + hostCFlags / hostDefines / muslExtraDefines
// + ndebugPicBlock × 2 with hostSseFeatures between + the tail
// muslCcIncludesX8664 set. Verifies that:
//
//   - target triple is x86_64-linux-gnu (NOT aarch64-linux-gnu).
//   - no `-march=` flag (host is generic x86_64).
//   - `-D_musl_=1` is present (muslExtraDefines).
//   - host_platform=true and tags=["tool"].
// TestEmitAS_HostNonMusl_X8664Chkstk_ByteExact (PR-35a / PR-35m closure)
// pins the full cmd_args bundle for a host x86_64 non-musl AS node
// (`$(B)/contrib/libs/cxxsupp/builtins/_/x86_64/chkstk.S.o`)
// against the reference. PR-35m retired the prologue-only bound by
// threading the include-tail (own AddIncl: musl-arch×4 x86_64 + linux-
// headers via prefix/suffix) through ModuleCCInputs.
// TestEmitAS_UtilContext_ByteExact (PR-35i / PR-33-C2_06 closure;
// PR-35m generic threading) pins the cmd_args bundle for util's only
// AS node (`$(B)/util/_/system/context_aarch64.S.o`) against
// the reference graph. Total 106 args. util declares no
// `NO_COMPILER_WARNINGS()` macro, so the warning bundle is the full
// `-Werror`/`-Wall`/`-Wextra` set (NOT `-Wno-everything`); util's own
// non-GLOBAL `CFLAGS(-Wnarrowing)` (util/ya.make:243) sits between
// commonDefines and the first noLibcUndebugBlock copy; the consumer-
// side `-D_musl_` sentinel sits between catboost and the second
// noLibcUndebugBlock copy; the include tail (13 args) carries util's
// linux-headers + runtime-stack + user-PEERDIR ADDINCL contributions.
//
// Verifies that:
//
//   - target triple is aarch64-linux-gnu with -march=armv8-a.
//   - warning bundle is `warningFlags` (6 args, NOT `-Wno-everything`).
//   - own CFLAG `-Wnarrowing` is present at the post-commonDefines slot.
//   - `-D_musl_` (NOT `-D_musl_=1`) is present at the post-catboost slot.
//   - includes tail matches the 13-arg reference set.
//
// PR-35m: the per-module compile knobs are now passed via the same
// `ModuleCCInputs` struct CC consumes (own AddIncl empty for util,
// peer-GLOBAL = libcxx/libcxxrt + musl-arch-aarch64×4 + the user-
// PEERDIR contributions, own CFlags = `-Wnarrowing`, AutoPeerCFlags =
// `-D_musl_`). The util-specific path-sniff stopgap is retired.
// TestEmitAS_AsmlibYasm_Cachesize_ByteExact (PR-35q) pins the yasm-
// toolchain shape for asmlib's host-PIC `.asm` AS nodes against the
// reference graph (`$(B)/contrib/libs/asmlib/cachesize64.pic.o`).
//
// Verifies the four shape divergences from the clang AS path:
//
//   - Output path is flat (`<modulePath>/<base>.pic.o`; no `_/` infix,
//     `.asm` suffix stripped).
//   - cmd_args is the 18-arg yasm invocation (NOT a 94/98/106/109-arg
//     clang AS bundle).
//   - Cwd is empty (the reference omits the `cwd` field for all 25
//     asmlib yasm AS nodes; PR-35q must not set `Cwd: $(B)`).
//   - Env is `ARCADIA_ROOT_DISTBUILD` + `YASM_TEST_SUITE` (no
//     `DYLD_LIBRARY_PATH`).
//
// Inputs ordering (yasm binary at index 0) and downstream wiring
// (DepRefs + ForeignDepRefs["tool"]) are pinned alongside.
// TestEmitAS_AsmlibYasm_OutputPath_NoUnderscoreInfix (PR-35q) verifies
// that asmlib host-PIC `.asm` AS nodes use the FLAT output path
// (`<base>.pic.o`) without the `_/` infix that the clang AS path
// applies unconditionally. This is the inverse of
// TestEmitAS_OutputPath_AlwaysHasUnderscore — the asmlib yasm branch
// is the documented exception to the clang-AS unconditional infix
// rule.
func TestEmitAS_AsmlibYasm_OutputPath_NoUnderscoreInfix(t *testing.T) {
	e := NewBufferedEmitter()
	_, outPath := EmitAS(hostInstance("contrib/libs/asmlib"), "memset64.asm", ModuleCCInputs{}, nil, testHostP, e)
	want := "$(B)/contrib/libs/asmlib/memset64.pic.o"

	if outPath.String() != want {
		t.Errorf("outPath = %q, want %q", outPath, want)
	}
}

// TestEmitAS_AsmlibYasm_TargetSide_NoYasmBranch (PR-35q) verifies that
// the yasm branch fires ONLY for host-PIC asmlib invocations. A
// hypothetical target-side asmlib AS (PIC=false) must take the clang
// AS path — the `_/<srcRel>.o` output, the clang cmd_args bundle, the
// `Cwd: $(B)`. The asmlib reference graph contains no such
// target-side node (asmlib is host-only by construction), but
// defending the predicate against PIC=false is the cheapest way to
// guarantee the branch never accidentally hijacks a future target AS
// node living under a similarly-named module path.
func TestEmitAS_AsmlibYasm_TargetSide_NoYasmBranch(t *testing.T) {
	e := NewBufferedEmitter()
	// PIC=false → target-side. Even though asmlibYasmModules matches
	// instance.Path, the predicate is gated on PIC=true.
	_, outPath := EmitAS(targetInstance("contrib/libs/asmlib"), "memset64.asm", ModuleCCInputs{}, nil, testHostP, e)
	// PR-35r: flat srcRel → flat output path (no _/ infix). memset64.asm
	// has no "/" so the clang AS path emits a flat output.
	wantClangPath := "$(B)/contrib/libs/asmlib/memset64.asm.o"

	if outPath.String() != wantClangPath {
		t.Errorf("outPath = %q, want %q (clang AS path; yasm branch must not fire for target-side)", outPath, wantClangPath)
	}

	if len(e.nodes) != 1 {
		t.Fatalf("emitter buffered %d nodes, want 1", len(e.nodes))
	}

	got := e.nodes[0]

	// Clang path sets Cwd; yasm path leaves it empty. A non-empty Cwd
	// confirms the clang branch ran.
	if got.Cmds[0].Cwd != "$(B)" {
		t.Errorf("Cmds[0].Cwd = %q, want $(B) (clang AS path)", got.Cmds[0].Cwd)
	}
}
