package main

// sources.go — per-source dispatch and include-closure helpers.
//
// emitOneSource is the per-source dispatch by extension that the
// genModule walker calls for each SRCS / GLOBAL_SRCS / JOIN_SRCS entry.
// It routes to EmitCC / EmitAS / EmitR5 / EmitR6 / EmitCF and returns
// the (NodeRef, output VFS, member inputs, primary count) tuple the
// caller folds into its AR / .global.a / LD aggregates.
//
// The closure helpers (walkClosure, joinSrcsIncludeClosure,
// jsCCIncludeInputs, jsTargetPeerAddIncl, resolveSourceVFS,
// includeScannerBasePaths) compose the per-source IncludeInputs used
// by the downstream CC compile.

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// sourceEmit is the emit-product of emitOneSource: a single CC/AS/R5/R6/CF/etc.
// node compiled from one declared source. nil = the source was silently
// skipped (a `.h` header that emitOneSource does not produce a node for, or
// an unhandled extension dispatched to the deferred-kind sink).
//
// PrimaryCount is the leading-CcIns count that names the member's primary
// source(s) — `.c/.cpp/.cc/.cxx/.S/.s/.asm` yield 1; `.rl6` yields 1 or 2
// (.rl6 source ± companion .h header).
type sourceEmit struct {
	Ref          NodeRef
	OutPath      VFS
	CcIns        []VFS
	PrimaryCount int
}

func emitOneSource(ctx *genCtx, instance ModuleInstance, srcDir string, srcRel string, in ModuleCCInputs, ancestorRebase bool) *sourceEmit {
	if isHeaderSource(srcRel) {
		return nil
	}

	// PR-30 D06: SRCDIR rebase is now ancestor-only and only fires when
	// the caller has decided this is the "include-from-parent" pattern
	// (PROGRAM whose SRCDIR is an ancestor of instance.Path; ragel6/bin
	// is the canonical case). LIBRARYs with SRCDIR (libcxxabi-parts,
	// musl_extra, tcmalloc/no_percpu_cache) keep
	// `srcInstance.Path == instance.Path`; the per-source SRCDIR
	// resolution happens inside EmitCC via `in.SrcDir`/`in.SourceRoot`
	// (composeCCPaths).
	srcInstance := instance

	if ancestorRebase {
		srcInstance.Path = srcDir
	}

	// When the instance is rebased to SRCDIR (ragel6/bin pattern), the
	// composer should NOT additionally apply SRCDIR routing — clear
	// SrcDir on the per-source input bag. When NOT rebased (LIBRARY
	// shape), keep SrcDir so the composer can decide local-vs-SRCDIR
	// resolution per source.
	srcIn := in
	if ancestorRebase {
		srcIn.SrcDir = ""
	}

	switch {
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"):
		// PR-31 D08: resolve the transitive include closure for
		// non-generated sources. Generated sources (handled in the
		// JS / R6 branches below — NOT this site) skip the scanner:
		// their primary input lives under $(B) and doesn't
		// exist on disk at scan time. The walker passes the
		// scanner-aware srcIn down to EmitCC.
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)

		// PR-M3-py3-runtime-closure: runtime_py3 __res.cpp / sitecustomize.cpp
		// each carry the matching .pyc.inc + PY_SRCS python inputs in REF.
		// PR-M3-L0-cascade-close-v2: lift the extras BEFORE resolving codegen
		// dep refs so the .pyc.inc producer (AR node) is reachable through the
		// IncludeInputs probe. Order is preserved (extras appended to the tail
		// of IncludeInputs) so EmitCC's inputs[] composition is unchanged.
		extras := runtimePy3CCExtraInputs(srcInstance.Path, srcRel)
		if len(extras) > 0 {
			srcIn.IncludeInputs = append(srcIn.IncludeInputs, extras...)
		}
		// Thread codegen producer dep refs into the CC node. PR-M3-L0-codegen-
		// deps-EV-PB extended this to PB/EV (with platform keying) on top of
		// the EN path established by PR-M3-module-tag-and-stats-enums-dep.
		srcIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, srcIn.IncludeInputs)

		ref, outPath := EmitCC(srcInstance, srcRel, srcIn, ctx.emit)

		// AR/LD aggregate the per-CC inputs (primary source +
		// resolved headers) into their own inputs slice per the
		// sg.json shape (PR-31 D11). Compose the input list here
		// (matching what EmitCC itself does internally).
		inputPath := emittedSourceInputPath(srcInstance, srcRel, srcIn, ctx.sourceRoot)
		ccInputs := append([]VFS{inputPath}, srcIn.IncludeInputs...)

		return &sourceEmit{Ref: ref, OutPath: outPath, CcIns: ccInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		// PR-28: when a host (`Flags.PIC`) `.S`/`.s` source belongs
		// to a module known to use yasm (`asmlibYasmModules`), recurse
		// into the host yasm PROGRAM and wire its LDRef into the AS
		// node's `ForeignDepRefs["tool"]` (matches reference: 25
		// host-asmlib AS nodes have foreign_deps.tool=yasm). Other
		// `.S` sources (target-side AS, host chkstk, host
		// libcxx/libcxxabi shims) pass nil — they assemble via
		// clang's built-in assembler with no foreign_deps.
		//
		// asmlib host walk is wired but not reached in the M2 archiver
		// closure because we peer contrib/libs/musl, not
		// contrib/libs/musl/full (the upstream PEERDIR rule
		// MUSL=yes && !MUSL_LITE → musl/full lives at
		// build/ymake.core.conf:1238-1245 and is not modelled here).
		// Closing the musl/full closure path is deferred to a follow-up
		// PR. The trigger code here remains as forward-scaffolding so
		// that PR will not need to re-derive the wiring; the existing
		// synthetic test pins it.
		var yasmRef *NodeRef

		// D41: dispatch on Target, not Flags.PIC; x86_64 IS the host axis in M2/M3.
		// PR-M3-F-5: extend yasm walk to all `.asm` sources on x86_64, not
		// just asmlibYasmModules. The reference graph uses yasm for every
		// `.asm` host source (util/system/context_x86.asm + asmlib's 25 nodes).
		if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
			const yasmPath = "contrib/tools/yasm"

			yasmInstance := NewToolInstance(ctx.host, yasmPath, instance.Language)
			yasmInstance.Flags = inferFlagsFromPath(yasmPath, true)

			yasmResult := genModule(ctx, yasmInstance)
			ldRef := yasmResult.LDRef
			yasmRef = &ldRef
		}

		// PR-31 D11: scan transitive headers for AS sources too. A
		// small subset of `.S` sources include `.h`/`.inc` headers
		// (e.g. cxxsupp/builtins/chkstk.S → assembly.h +
		// int_endianness.h); the scanner populates the AS node's
		// inputs and feeds the downstream AR's memberInputs aggregator.
		asIn := srcIn
		asIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		// PR-35m: thread the full ModuleCCInputs into EmitAS so it
		// can compose own/peer ADDINCL, own non-GLOBAL CFLAGS, and
		// auto peer CFLAGS at the same slots CC consumes them
		// (retiring the util-specific path-sniff stopgap PR-35i added).
		ref, outPath := EmitAS(srcInstance, srcRel, asIn, yasmRef, ctx.emit)

		// PR-35y R8: when the module declares SRCDIR and the .S
		// source does not exist locally at instance.Path/<srcRel>,
		// the AR memberInput resolves at `$(S)/<srcDir>/<srcRel>`
		// rather than the unrebased `<instance.Path>/<srcRel>`.
		// Empirical reference: tcmalloc/no_percpu_cache (SRCDIR=
		// `contrib/libs/tcmalloc`) — its `tcmalloc/internal/percpu_rseq_asm.S`
		// resolves at `contrib/libs/tcmalloc/tcmalloc/internal/percpu_rseq_asm.S`,
		// not `contrib/libs/tcmalloc/no_percpu_cache/tcmalloc/internal/percpu_rseq_asm.S`.
		// Same rule as composeASPaths' SRCDIR routing for AS itself
		// (PR-35r, as.go:316-336): keeping the gen.go aggregator's
		// path in sync with as.go's resolution.
		asInputRel := srcInstance.Path + "/" + srcRel

		if srcDir != "" && srcDir != srcInstance.Path && !sourceExistsLocally(ctx.sourceRoot, srcInstance.Path, srcRel) {
			asInputRel = srcDir + "/" + srcRel
		}

		// PR-M3-openssl-ar-plugin-and-as-clean: collapse `..` segments so
		// e.g. openssl's `crypto/../asm/...` resolves to `asm/...` in the
		// AR aggregator's memberInputs. The AS node's own input path is
		// composed independently inside as.go and is already cleaned.
		asInputRel = path.Clean(asInputRel)

		asInputs := append([]VFS{Source(asInputRel)}, asIn.IncludeInputs...)

		return &sourceEmit{Ref: ref, OutPath: outPath, CcIns: asInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".rl6"):
		// Host-ragel6 recursion (D31, eager per PR-28). The recursion
		// happens here so the resulting LD's outputs[0] can be
		// threaded into EmitR6's cmd_args[0] (PR-28-D01 — internal
		// consistency between R6 invocation path and our own host LD).
		//
		// `contrib/tools/ragel6/bin` is the real host-PROGRAM
		// directory; the parent `contrib/tools/ragel6/ya.make` uses
		// INCLUDE(${ARCADIA_ROOT}/...) which our parser does not yet
		// expand (M5+ variable substitution work).
		const ragelBinPath = "contrib/tools/ragel6/bin"

		// Fallback ragel6 path: used when the host walk fails its
		// parse. The literal matches the reference graph's invocation
		// path, so a zero-host-LD codepath at least produces a
		// meaningful argv even though the host LD node is missing.
		var ragelFallbackPath = Build("contrib/tools/ragel6/ragel6").String()

		var (
			ragelLDRef     NodeRef
			ragelBinaryStr = ragelFallbackPath
		)

		ragelInstance := NewToolInstance(ctx.host, ragelBinPath, instance.Language)
		ragelInstance.Flags = inferFlagsFromPath(ragelInstance.Path, true)

		if exc := Try(func() {
			ragelResult := genModule(ctx, ragelInstance)
			ragelLDRef = ragelResult.LDRef
			ragelBinaryStr = ragelResult.LDPath
		}); exc != nil {
			// Only swallow *ParseError — the documented gap when
			// ragel6's ya.make contains INCLUDE(${ARCADIA_ROOT}/...)
			// that our parser cannot yet expand (M5+ variable
			// substitution). Any other exception is unexpected and
			// must propagate loudly rather than silently produce a
			// zero ragel6LD ref.
			var pe *ParseError

			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}

			// Leave ragelLDRef zero-valued and ragelBinaryStr at the
			// reference-shaped fallback; document the host-tool gap
			// rather than re-throwing. The R6 node will not dep-link
			// to a host ragel6, but its cmd_args[0] still names a
			// plausible binary path.
		}

		// PR-35z: scan the `.rl6` source's transitive #include closure
		// (the `.rl6` body embeds `#include` directives that resolve
		// through the same search-path / sysincl rules as `.cpp`/`.S`
		// sources). Both the R6 generator node AND the downstream CC
		// of the generated `.cpp` carry the same closure in their
		// `inputs` slot — reference graph: util/datetime/parser.rl6
		// produces a 1009-input R6 node and a 1009-input CC node,
		// where positions 1.. of each are identical (the `.rl6`
		// source plus its 1007-header transitive closure).
		rl6Closure := walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)

		// PR-M3-final-r6-stats-enums-leak: REF's R6 closure resolves a
		// transitive `#include <..._serialized.h>` directive (via stats.h
		// or sibling) for its descendant headers (e.g. util/generic/
		// serialized_enum.h) but does NOT add the generated EN
		// `_serialized.{cpp,h}` siblings themselves to the R6 inputs.
		// Our codegen registry resolves the directive to the registered
		// $(B)/<...>_serialized.h output and follows EmitsIncludes,
		// which pulls in both the .h itself and its sibling .cpp. Strip
		// both at the R6 input boundary; the descendant util headers
		// (which REF does carry) reach R6 inputs through the same
		// EmitsIncludes traversal and are unaffected.
		rl6Closure = filterEnSerializedSiblings(rl6Closure)

		r6Ref, r6Out := EmitR6(srcInstance, srcRel, ragelLDRef, ragelBinaryStr, srcIn.Ragel6Flags, rl6Closure, ctx.emit)

		// F-7-B / PR-AUDIT-2 D02: register the R6 output (.rl6.cpp). Ragel emits
		// the .rl6 source's `#include` directives verbatim into the generated
		// .cpp, so the .cpp's effective direct-include set is the .rl6's. We
		// register a single EmitsIncludes entry pointing at the .rl6 source;
		// WalkClosure on the .rl6.cpp will recurse into the .rl6 via the
		// FS-parsed locator and produce the same closure the downstream CC
		// previously got from scanning the .rl6 manually.
		rl6SourceVFS := Source(srcInstance.Path + "/" + srcRel)
		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "R6",
				OutputPath:    r6Out,
				EmitsIncludes: []VFS{rl6SourceVFS},
			})
		}

		// PR-29-D07: same shape as the JS branch above. Pass
		// IsGenerated so the downstream CC composes inputPath under
		// $(B)/<srcInstance.Path>/<rel> rather than the
		// stale $(S) shape. PR-30 D04: thread r6Ref as the
		// downstream CC's `Generator` so the CC node carries an
		// explicit dep on its R6 source-generator node, matching the
		// reference shape.
		//
		// PR-AUDIT-2 D02: dispatch through the unified VFS-path entry — the
		// .rl6.cpp is registered in the codegen registry (see Register above)
		// and the scanner walks transitively through both BUILD_ROOT and
		// SOURCE_ROOT children uniformly. Previously this site assembled
		// `[<.rl6 source>, ...rl6Closure]` by hand from a separate
		// source-side scan; the architecturally-correct shape comes from
		// WalkClosure rooted at the generated .cpp.
		ccSrcRel := strings.TrimPrefix(r6Out.Rel, srcInstance.Path+"/")
		ccIncludeInputs := walkClosure(ctx, srcInstance, r6Out, srcIn)

		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.Generator = r6Ref
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = ccIncludeInputs
		// PR-41 Fix H: ymake's _LANG_CFLAGS_RL=-Wno-implicit-fallthrough applies to CC
		// compiles whose source is a .rl6-generated .cpp (build/ymake.core.conf).
		// Extend in M3+ for .pyx, .py.py3, .rl5 when their closures surface.
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		// PR-M3-L0-codegen-deps-EV-PB: thread EN/PB/EV producer refs reached
		// through the .rl6.cpp's transitive include closure. Generator (r6Ref)
		// is filtered out so EmitCC's leading-DepRefs slot isn't duplicated.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r6Ref)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.emit)

		// R6-derived CC: primary input is the BUILD_ROOT-rooted .cpp
		// generated by ragel6. No scanner pass (the .cpp doesn't exist
		// on disk at scan time).
		//
		// PR-35y R7: the AR/LD member-inputs aggregator excludes the
		// BUILD_ROOT-staged generated cpp (mirror of the JS rule) and
		// instead carries the original `.rl6` source plus its
		// companion `.h` header. Reference graph confirms: util's
		// libyutil.a inputs include `parser.rl6` and `parser.h`,
		// never the `parser.rl6.cpp` BUILD_ROOT shim. The companion
		// `.h` header is added only when a sibling file with the
		// same basename and `.h` suffix exists on disk — the
		// convention holds for every observed `.rl6` source in the
		// M2 closure (util/datetime/parser.rl6 → parser.h).
		ccInputs := []VFS{rl6SourceVFS}
		primaryCount := 1

		companionRel := strings.TrimSuffix(srcRel, ".rl6") + ".h"
		companionAbs := filepath.Join(ctx.sourceRoot, srcInstance.Path, companionRel)

		if info, err := os.Stat(companionAbs); err == nil && !info.IsDir() {
			ccInputs = append(ccInputs, Source(srcInstance.Path+"/"+companionRel))
			primaryCount = 2
		}

		// PR-M3-AR-header-closure: roll up the downstream CC's transitive
		// header closure into memberInputs so the AR/LD aggregator carries
		// the libcxx/musl/protobuf/etc. headers the generated .rl6.cpp
		// includes. Upstream ymake propagates each member-CC's NodeInputs
		// up via EDT_BuildFrom (json_visitor.cpp:788-789 NeedToPassInputs).
		ccInputs = append(ccInputs, ccIncludeInputs...)

		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: ccInputs, PrimaryCount: primaryCount}

	case strings.HasSuffix(srcRel, ".ev"):
		// PR-M3-C: .ev sources in a LIBRARY module (e.g. devtools/ymake/diag/trace.ev).
		// Emits one EV node (generating .ev.pb.cc + .ev.pb.h) then a downstream
		// CC node compiling the generated .ev.pb.cc. The CC node's full include
		// closure is not scanned (generated files don't exist on disk at gen time);
		// the node structure is correct at L0/L1/L2 even without L3-exact inputs.
		{
			// Walk host tool programs.
			cppStyleguideBinary := pbCppStyleguidePath
			protocBinary := pbProtocBinaryPath
			event2cppBinary := evEvent2cppBinaryPath

			var cppStyleguideLDRef, protocLDRef, event2cppLDRef NodeRef

			protocHostInst := NewToolInstance(ctx.host, pbProtocModule, instance.Language)
			protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

			if exc := Try(func() {
				result := genModule(ctx, protocHostInst)
				protocLDRef = result.LDRef
				protocBinary = result.LDPath
			}); exc != nil {
				_ = exc
			}

			cppStyleguideHostInst := NewToolInstance(ctx.host, pbCppStyleguideModule, instance.Language)
			cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

			if exc := Try(func() {
				result := genModule(ctx, cppStyleguideHostInst)
				cppStyleguideLDRef = result.LDRef
				cppStyleguideBinary = result.LDPath
			}); exc != nil {
				_ = exc
			}

			event2cppHostInst := NewToolInstance(ctx.host, evEvent2cppModule, instance.Language)
			event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

			if exc := Try(func() {
				result := genModule(ctx, event2cppHostInst)
				event2cppLDRef = result.LDRef
				event2cppBinary = result.LDPath
			}); exc != nil {
				_ = exc
			}

			// moduleTag is empty for LIBRARY modules (no "cpp_proto" tag).
			evRef := EmitEV(
				srcInstance, srcRel,
				cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				"", ctx.sourceRoot, ctx.emit)

			// F-7-B: register the .ev.pb.h output with EmitsIncludes from the .ev imports,
			// plus the protobuf runtime headers (F-7-D).
			evRelPath := srcInstance.Path + "/" + srcRel
			evH := Build(evRelPath + ".pb.h")
			evPbCC := Build(evRelPath + ".pb.cc")

			// PR-M3-L0-codegen-deps-EV-PB: stash the EV NodeRef under both outputs
			// on the emitting platform so consumer CCs in OTHER modules whose
			// IncludeInputs include this .ev.pb.h / .ev.pb.cc dep on the producer.
			evKey := codegenOutputKey{platform: srcInstance.Platform.Target}
			evKey.path = evH
			ctx.evOutputs[evKey] = evRef
			evKey.path = evPbCC
			ctx.evOutputs[evKey] = evRef
			if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath)
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
				evEmitsIncludes := make([]VFS, 0, len(directImports)+len(protobufRuntimeHeaders)+len(evExtras))
				evEmitsIncludes = append(evEmitsIncludes, directImports...)
				evEmitsIncludes = append(evEmitsIncludes, protobufRuntimeHeaders...)
				evEmitsIncludes = append(evEmitsIncludes, evExtras...)
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evH,
					EmitsIncludes: evEmitsIncludes,
				})
				// PR-AUDIT-2 D04: register the .ev.pb.cc output too. event2cpp
				// emits a `#include "<base>.ev.pb.h"` plus the protobuf runtime
				// headers; the .pb.h's own EmitsIncludes are already registered
				// (just above), so a single entry pointing at the .pb.h would
				// suffice — we mirror the .pb.h list for symmetry with PB (the
				// .pb.cc emitted by protoc includes the same runtime headers).
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evPbCC,
					EmitsIncludes: append([]VFS{evH}, protobufRuntimeHeaders...),
				})
			}

			// Emit downstream CC for the generated .ev.pb.cc.
			// PR-AUDIT-2 D04: dispatch through the unified VFS-path entry —
			// the .ev.pb.cc is registered above with the right EmitsIncludes;
			// WalkClosure walks transitively into the .pb.h and out to the
			// protobuf runtime headers via the FS locator.
			evPbCCSuffix := srcRel + ".pb.cc"
			ccIn := srcIn
			ccIn.IsGenerated = true
			ccIn.Generator = evRef
			ccIn.HasGenerator = true
			ccIn.IncludeInputs = walkClosure(ctx, srcInstance, evPbCC, srcIn)
			// PR-M3-final-surgical (fix 1): the .ev.pb.cc.o consumer must not
			// carry its OWN .ev.pb.h in inputs[] (REF omits the self-include;
			// cross-imported sibling .ev.pb.h entries remain). The walker
			// reaches evH transitively because the .pb.cc is registered with
			// evH as its first EmitsIncludes child — drop just that entry.
			{
				filtered := make([]VFS, 0, len(ccIn.IncludeInputs))
				for _, in := range ccIn.IncludeInputs {
					if in == evH {
						continue
					}
					filtered = append(filtered, in)
				}
				ccIn.IncludeInputs = filtered
			}
			wireFormatVFS := Source(pbRuntimeBase + "google/protobuf/wire_format.h")
			// PR-M3-final-codegen-registry-expansion: protoc emits
			// `#include "google/protobuf/wire_format.h"` directly. Add to inputs
			// only on this CC node (not via registry — that would over-emit).
			ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
			// PR-M3-L0-codegen-deps-EV-PB: thread cross-codegen producer refs
			// (e.g. an .ev that imports another module's .proto pulls the
			// peer's PB into the consumer CC's deps via its .pb.h in inputs[]).
			ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, evRef)

			ref, outPath := EmitCC(srcInstance, evPbCCSuffix, ccIn, ctx.emit)

			// The primary input for the AR/LD memberInputs is the original .ev source.
			// PR-M3-final-codegen-registry-expansion: wire_format.h also propagates
			// up into the AR rollup (matched in REF on libdevtools-ymake-diag.a).
			return &sourceEmit{
				Ref:          ref,
				OutPath:      outPath,
				CcIns:        []VFS{Source(srcInstance.Path + "/" + srcRel), wireFormatVFS},
				PrimaryCount: 1,
			}
		}

	case strings.HasSuffix(srcRel, ".rl"):
		// PR-M3-E: ragel5 two-step code generation (.rl → .rl.tmp → .rl5.cpp).
		// Mirrors the .rl6 branch: walk the two host ragel5 PROGRAMs eagerly,
		// emit the R5 node, then emit a CC node for the generated .rl5.cpp.
		const (
			ragel5Path  = "contrib/tools/ragel5/ragel"
			rlgenCdPath = "contrib/tools/ragel5/rlgen-cd"
		)
		var (
			ragel5Fallback  = Build("contrib/tools/ragel5/ragel/ragel5").String()
			rlgenCdFallback = Build("contrib/tools/ragel5/rlgen-cd/rlgen-cd").String()
		)

		var (
			ragel5LDRef   NodeRef
			rlgenCdLDRef  NodeRef
			ragel5BinStr  = ragel5Fallback
			rlgenCdBinStr = rlgenCdFallback
		)

		ragel5Instance := NewToolInstance(ctx.host, ragel5Path, srcInstance.Language)
		ragel5Instance.Flags = inferFlagsFromPath(ragel5Path, true)

		if exc := Try(func() {
			res := genModule(ctx, ragel5Instance)
			ragel5LDRef = res.LDRef
			ragel5BinStr = res.LDPath
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}

		rlgenCdInstance := NewToolInstance(ctx.host, rlgenCdPath, srcInstance.Language)
		rlgenCdInstance.Flags = inferFlagsFromPath(rlgenCdPath, true)

		if exc := Try(func() {
			res := genModule(ctx, rlgenCdInstance)
			rlgenCdLDRef = res.LDRef
			rlgenCdBinStr = res.LDPath
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}

		r5Ref, r5TmpOut, r5CppOut := EmitR5(srcInstance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinStr, rlgenCdBinStr, ctx.emit)
		_ = r5Ref

		// F-7-B / PR-AUDIT-2 D05: register R5 outputs. ragel5 emits the
		// .rl source's #include directives verbatim into the generated
		// .rl5.cpp; the .tmp intermediate has no consumer-visible includes.
		// PR-M3-L0-cascade-close-v2: ProducerRef = r5Ref so the downstream
		// CC consuming the .rl5.cpp threads R5 into its deps[].
		rlSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "R5",
				OutputPath:     r5TmpOut,
				EmitsIncludes:  nil,
				ProducerRef:    r5Ref,
				HasProducerRef: true,
			})
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "R5",
				OutputPath:     r5CppOut,
				EmitsIncludes:  []VFS{rlSourceVFS},
				ProducerRef:    r5Ref,
				HasProducerRef: true,
			})
		}

		// Downstream CC for the generated .rl5.cpp.
		// PR-AUDIT-2 D05: dispatch through the unified VFS-path entry —
		// the .rl5.cpp is registered above with the .rl source as its
		// single direct include; WalkClosure recurses into the .rl via
		// the FS locator and yields the full transitive closure.
		ccSrcRel := strings.TrimPrefix(r5CppOut.Rel, srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IsGenerated = true
		ccClosure := walkClosure(ctx, srcInstance, r5CppOut, srcIn)
		// PR-M3-multi-output-producer-siblings: ragel5 emits two outputs
		// (.rl.tmp intermediate + .rl5.cpp). Upstream ymake lists BOTH
		// sibling outputs in the downstream CC's inputs[] — the consumer
		// depends on the producer node, so every produced file is a
		// dependency edge. walkClosure scans only the .rl5.cpp (and its
		// transitive includes), so the .rl.tmp must be injected
		// explicitly. Prepended to keep the multi-output sibling adjacent
		// to the primary .rl5.cpp input in DFS order. The .rl.tmp does
		// NOT propagate up into AR/LD memberInputs (sg2.json shows only
		// the .rl source rolls up; the .tmp is an intermediate that does
		// not cross module boundaries).
		ccIn.IncludeInputs = append([]VFS{r5TmpOut}, ccClosure...)
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		// PR-M3-L0-codegen-deps-EV-PB: thread EN/PB/EV producer refs reached
		// through the .rl5.cpp's transitive include closure.
		// PR-M3-L0-cascade-close-v2: prepend r5Ref. WalkClosure skips the
		// root (r5CppOut) so the registry probe alone wouldn't surface R5;
		// REF's R5-derived CC carries R5 as its leading dep.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r5Ref)
		ccIn.ExtraDepRefs = append([]NodeRef{r5Ref}, ccIn.ExtraDepRefs...)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.emit)

		// AR/LD member inputs: use the original .rl source (not generated .cpp).
		// PR-M3-AR-header-closure: roll up the downstream CC's transitive
		// header closure into memberInputs. Upstream ymake propagates each
		// member-CC's NodeInputs up to the parent AR/LD via EDT_BuildFrom
		// (json_visitor.cpp:788-789 NeedToPassInputs); the .rl-generated
		// .cpp's #include closure is included even though the AR archives
		// only the .o, because the inputs walk is set-union over all
		// transitive file deps under the module boundary. Uses ccClosure
		// (NOT ccIn.IncludeInputs) so the .rl.tmp sibling stays scoped to
		// the CC consumer and does not bleed into the AR/LD rollup.
		rlMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, ccClosure...)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: rlMemberInputs, PrimaryCount: 1}

	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		// PR-M3-E: CONFIGURE_FILE template source. Emit a CF node that runs
		// configure_file.py to expand @VAR@ placeholders, then emit a CC
		// node for the generated .cpp / .c file.
		//
		// The CF node's cmd_args include the DEFAULT-declared cfg vars; those
		// are passed via the moduleData in srcIn.DefaultVars (set by genModule
		// before calling emitOneSource). We also add BUILD_TYPE=DEBUG (the
		// hardcoded build configuration).
		//
		// The output path strips the .in suffix: sandbox.cpp.in → sandbox.cpp.
		// PR-M3-F-5: scan the .in template for its transitive include closure
		// (same as a .cpp source) and fold into srcIn.IncludeInputs before
		// calling EmitCF so the CF node's inputs[] matches the reference shape
		// (e.g. sandbox.cpp.in → 795-entry closure; build_info.cpp.in → 5).
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		cfRef, cfOut := EmitCF(srcInstance, srcRel, srcIn, ctx.emit)

		// F-7-B / PR-AUDIT-2 D08: register the CF output. configure_file.py
		// performs `@VAR@` substitution but leaves `#include` directives
		// intact, so the generated .cpp's direct includes are the .cpp.in's
		// (modulo substitution). We register the .cpp.in source as the
		// single EmitsIncludes child so WalkClosure recurses into it via
		// the FS locator and yields the full transitive closure that the
		// downstream CC needs.
		// PR-M3-final-codegen-registry-expansion: configure_file.py is the
		// codegen script driving the CF node; REF wires it as an input on
		// every CC consumer of the generated .cpp (verified on
		// build_info.cpp.o and sandbox.cpp.o).
		// PR-M3-L0-cascade-close-v2: ProducerRef = cfRef so downstream CC's
		// resolveCodegenDepRefs threads the CF producer into its deps[].
		inSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "CF",
				OutputPath:     cfOut,
				EmitsIncludes:  []VFS{inSourceVFS, configureFilePyVFS},
				ProducerRef:    cfRef,
				HasProducerRef: true,
			})
		}

		// Downstream CC for the generated .cpp / .c.
		// PR-AUDIT-2 D08: dispatch through the unified VFS-path entry —
		// the .cpp is registered above with the .cpp.in as its single
		// direct include; WalkClosure recurses into the .cpp.in via the
		// FS locator and yields the full transitive closure.
		ccSrcRel := strings.TrimPrefix(cfOut.Rel, srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, cfOut, srcIn)
		// PR-M3-L0-codegen-deps-EV-PB: thread codegen producer refs reached
		// through the CF-generated .cpp's transitive include closure.
		// PR-M3-L0-cascade-close-v2: also add cfRef directly — the CC
		// compiles cfOut, and WalkClosure skips the root (cfOut itself),
		// so the registry probe wouldn't find it via IncludeInputs alone.
		// REF's CF-derived CC carries the CF producer as a leading dep
		// (sandbox.cpp.o → CF sandbox.cpp).
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, cfRef)
		ccIn.ExtraDepRefs = append([]NodeRef{cfRef}, ccIn.ExtraDepRefs...)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.emit)

		// AR/LD member inputs: use the original .cpp.in / .c.in source.
		// PR-M3-AR-header-closure: roll up the downstream CC's transitive
		// header closure (the closure of the generated .cpp scanned against
		// the .cpp.in body via the codegen-registry EmitsIncludes edge) so
		// the AR/LD aggregator carries the same set upstream ymake propagates
		// via EDT_BuildFrom (json_visitor.cpp:788-789 NeedToPassInputs).
		cfMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, ccIn.IncludeInputs...)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: cfMemberInputs, PrimaryCount: 1}
	}

	// PR-M3-A: known-deferred source kinds are silently skipped rather
	// than throwing. Real emitters land in PR-M3-B (PB), PR-M3-D (EN/EV),
	// and later PRs. Until then, returning false means the source
	// contributes nothing to the AR/LD node set; the module may become
	// header-only if all its sources are deferred.
	if isSkippedSource(srcRel) {
		return nil
	}

	ThrowFmt("gen: %s: unsupported source extension in %q", instance.Path, srcRel)

	return nil
}

// emittedSourceInputPath mirrors composeCCPaths' inputPath logic so
// the walker can compose the AR/LD inputs aggregator without having
// to round-trip through EmitCC's emitted node. Returns the
// `$(S)/...` (or `$(B)/...` for IsGenerated)
// path the CC node will use as its primary input.
func emittedSourceInputPath(instance ModuleInstance, srcRel string, in ModuleCCInputs, sourceRoot string) VFS {
	if in.IsGenerated {
		return Build(instance.Path + "/" + srcRel)
	}

	if in.SrcDir != "" && in.SrcDir != instance.Path {
		localCandidate := filepath.Join(sourceRoot, instance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			return Source(in.SrcDir + "/" + srcRel)
		}
	}

	return Source(instance.Path + "/" + srcRel)
}

// joinSrcsIncludeClosure unions per-source #include closures across
// `sources` (PR-35d) using the consumer's own scan context. The
// scanner's DFS runs over all members with a SHARED visited set —
// mirroring the actual joined .cpp compile, where headers reached
// once stay deduped — so total work is O(union closure) not O(sum
// per-source closures). Returns nil when nothing resolves.
// joinSrcsIncludeClosure walks the include graph for a JOIN_SRCS member
// set. `scanPlatform` chooses which scanner + arch search-paths to use:
// callers pass `srcInstance.Platform` for the normal case; the JS-target
// override (PR-35s) passes `ctx.target` so the closure resolves against
// the target-arch musl layout even when the surrounding walk is host-axis.
// The instance itself is read for module-level facts (Path, Flags.LibcMusl)
// — its Platform identity is NOT mutated.
func joinSrcsIncludeClosure(ctx *genCtx, scanPlatform *Platform, srcInstance ModuleInstance, sources []string, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerForPlatform(scanPlatform)
	if scanner == nil {
		return nil
	}

	visited := NewVFSSet(1024)
	order := make([]VFS, 0, 1024)
	srcAbsSet := make(map[VFS]struct{}, len(sources))

	for _, src := range sources {
		srcRelOnDisk := srcInstance.Path + "/" + src

		if in.SrcDir != "" && in.SrcDir != srcInstance.Path {
			localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, src)
			info, err := os.Stat(localCandidate)

			if err != nil || info.IsDir() {
				srcRelOnDisk = in.SrcDir + "/" + src
			}
		}

		cfg := ScanContext{
			SourceRel:       srcRelOnDisk,
			OwnAddIncl:      in.AddIncl,
			PeerAddInclSet:  in.PeerAddInclGlobal,
			BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.LibcMusl, scanPlatform),
		}

		// PR-M3-perf-E: scanCtx dispatch — local vs interned (see
		// genCtx.getScanCtx). Within this join-srcs loop every source's
		// cfg differs only in SourceRel; PR-M3-perf-E ignored that
		// observation in favour of routing through getScanCtx anyway,
		// which yields one scanCtx per unique (ctxHash) and lets
		// resolveCache / subgraphCache entries from earlier sources serve
		// later sources at the same ctxHash.
		sc := ctx.getScanCtx(scanner, cfg)

		// `WalkSource` rewrites `sc.cfg.SourceRel` to the current
		// source-rel so sysinclSourceLookup keys on the right path. We
		// must therefore use the dfs entry that ALSO sets it, OR set it
		// inline before dfs. dfs reads sc.cfg.SourceRel for srcClassHash,
		// so set it here before invoking dfs against the shared visited+order.
		sc.cfg.SourceRel = srcRelOnDisk

		// Scanner walks operate on VFS values; the FS translation
		// happens at parseIncludes / fileExists.
		srcAbs := Source(srcRelOnDisk)
		srcAbsSet[srcAbs] = struct{}{}
		sc.dfs(srcAbs, visited, &order)
	}

	if len(order) == 0 {
		return nil
	}

	out := make([]VFS, 0, len(order))

	for _, abs := range order {
		// Skip the source files themselves — JOIN_SRCS members are
		// emitted separately as Source(<path>/<src>); the scanner
		// closure carries only headers/extras.
		if _, isSrc := srcAbsSet[abs]; isSrc {
			continue
		}

		out = append(out, abs)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// jsCCIncludeInputs assembles `[scripts..., sources..., closure...]`
// for the JS-derived CC's IncludeInputs slot (PR-35d).
func jsCCIncludeInputs(srcInstance ModuleInstance, sources []string, closure []VFS) []VFS {
	out := make([]VFS, 0, 2+len(sources)+len(closure))
	out = append(out, Source("build/scripts/gen_join_srcs.py"))
	out = append(out, Source("build/scripts/process_command_files.py"))

	for _, s := range sources {
		out = append(out, Source(srcInstance.Path+"/"+s))
	}

	out = append(out, closure...)

	return out
}

// jsTargetPeerAddIncl rebases a host (x86_64) PeerAddInclGlobal slice to
// the target (aarch64) musl arch layout for use in the JS-node closure
// scan. JS nodes are anchored to the target platform axis (PR-35s), so
// their include closure must reflect aarch64 musl search paths rather
// than the host x86_64 ones that the surrounding HOST-build moduleInputs
// carries.
//
// PR-40 Fix C: narrow shim — only the musl arch/x86_64 entry is
// rewritten to arch/aarch64; all other entries pass through unchanged.
// TODO: remove when a general target-addincl propagation mechanism lands
// in M3+ (the same milestone as the BinaryDir lift for Fix D).
func jsTargetPeerAddIncl(hostPeerAddIncl []string) []string {
	const (
		hostMuslArch   = "contrib/libs/musl/arch/x86_64"
		targetMuslArch = "contrib/libs/musl/arch/aarch64"
	)

	out := make([]string, len(hostPeerAddIncl))

	for i, p := range hostPeerAddIncl {
		if p == hostMuslArch {
			out[i] = targetMuslArch
		} else {
			out[i] = p
		}
	}

	return out
}

// resolveSourceVFS composes the `$(S)/...` VFS path of a
// SRCS-declared source, applying composeCCPaths' SRCDIR-aware
// fallback: when the module declares SRCDIR and no local file exists
// at instance.Path/<srcRel>, the source resolves under SRCDIR. This
// is registration-time path resolution (matches AUDIT-3 bucket (B));
// the os.Stat is legitimate at this layer because the answer feeds
// path composition, not scanner-internal locator dispatch.
func resolveSourceVFS(ctx *genCtx, srcInstance ModuleInstance, srcRel string, srcDir string) VFS {
	srcRelOnDisk := srcInstance.Path + "/" + srcRel

	if srcDir != "" && srcDir != srcInstance.Path {
		localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			srcRelOnDisk = srcDir + "/" + srcRel
		}
	}

	return Source(srcRelOnDisk)
}

// resolveCodegenDepRefs replaced by the EN/PB/EV-aware version at line 344
// (PR-M3-L0-codegen-deps-EV-PB).

// walkClosure resolves the transitive include closure of a source
// rooted at any VFS path — `$(S)/...` for FS-resident
// sources or `$(B)/...` for codegen outputs whose producer
// has registered an EmitsIncludes entry in the per-scanner
// CodegenRegistry. The scanner's locator (forEachResolvedChild)
// dispatches FS-vs-codegen internally; callers do not branch on
// is-on-disk. Returns the resolved include set or nil when the
// scanner is unavailable.
//
// The ScanContext mirrors what cmd_args -I uses: own AddIncl + peer
// GLOBAL AddIncl + the cc bundle's implicit baseline (linux-headers
// and the active musl-arch include path).
func walkClosure(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerFor(srcInstance)
	if scanner == nil {
		return nil
	}

	// SourceRel feeds srcClassHash (per-source subgraph-cache keying).
	// WalkClosure overwrites it per-call for SOURCE_ROOT paths so
	// scanCtx reuse across sources keys correctly; for BUILD_ROOT
	// paths it stays as set here and is never consulted by the
	// BUILD_ROOT child branch.
	cfg := ScanContext{
		SourceRel:       vfsPath.Rel,
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.LibcMusl, srcInstance.Platform),
	}

	sc := ctx.getScanCtx(scanner, cfg)

	return sc.WalkClosure(vfsPath)
}

// includeScannerBasePaths returns the implicit include search path
// that the cc bundle adds via cmd_args (SOURCE_ROOT + linux-headers +
// musl arch when applicable). The scanner uses these as fallback
// resolution candidates so headers like `<util/folder/path.h>` (repo-
// rooted system-form includes) and `<linux/types.h>` (linux-headers)
// resolve in the same way the compiler would.
//
// Non-musl flavours: an empty-string entry is prepended first,
// representing the SOURCE_ROOT itself. The resolver treats an empty
// prefix as "resolve directly against SOURCE_ROOT" — so `<util/foo.h>`
// tries $(S)/util/foo.h before the linux-headers subtree.
// This mirrors the `-I$(S)` flag the compiler receives via
// cmd_args for every non-musl CC node.
//
// Musl flavours (composeMuslCC / composeMuslHostCC paths) MUST NOT get
// the empty prefix — they use `-nostdinc` and have a fully explicit
// include search path via muslCcIncludes. Adding SOURCE_ROOT there
// would cause false resolution of system-form includes against the
// repo root, silently expanding the musl CC input sets incorrectly.
// includeScannerBasePaths returns the base search-path list for the
// include scanner. `libcMusl` is the per-MODULE flag (this module is
// part of contrib/libs/musl/*); `scanPlatform` is the platform the
// search paths resolve against (typically `instance.Platform`, but
// JOIN_SRCS during a host walk passes `ctx.target` to force the
// target-arch musl-arch paths without mutating the surrounding
// instance).
func includeScannerBasePaths(libcMusl bool, scanPlatform *Platform) []string {
	base := []string{
		"contrib/libs/linux-headers",
		"contrib/libs/linux-headers/_nf",
	}

	if libcMusl {
		// Mirror muslCcIncludes / muslCcIncludesX8664: arch + generic
		// + src/include + src/internal + include + extra.
		muslPaths := []string{
			"contrib/libs/musl/arch/" + string(scanPlatform.ISA),
			"contrib/libs/musl/arch/generic",
			"contrib/libs/musl/src/include",
			"contrib/libs/musl/src/internal",
			"contrib/libs/musl/include",
			"contrib/libs/musl/extra",
		}

		// Musl paths come BEFORE linux-headers in the cmd_args ordering.
		out := make([]string, 0, len(muslPaths)+len(base))
		out = append(out, muslPaths...)
		out = append(out, base...)

		return out
	}

	// Non-musl: prepend the empty-prefix entry (SOURCE_ROOT itself) so
	// repo-rooted system-form includes like `<util/folder/path.h>`
	// resolve against $(S)/util/folder/path.h.
	out := make([]string, 0, 1+len(base))
	out = append(out, "")
	out = append(out, base...)

	return out
}
