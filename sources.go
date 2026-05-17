package main

// sources.go — per-source dispatch and include-closure helpers.
//
// emitOneSource dispatches by extension for each SRCS / GLOBAL_SRCS /
// JOIN_SRCS entry, routing to EmitCC / EmitAS / EmitR5 / EmitR6 /
// EmitCF and returning the (NodeRef, output VFS, member inputs,
// primary count) tuple. Closure helpers (walkClosure / joinSrcs /
// jsCC / jsTargetPeerAddIncl / resolveSourceVFS /
// includeScannerBasePaths) compose per-source IncludeInputs.

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// sourceEmit is the emit-product of emitOneSource: a single
// CC/AS/R5/R6/CF/etc. node from one declared source. nil = silently
// skipped (e.g. `.h` headers, deferred-kind sources). PrimaryCount is
// the leading-CcIns count naming the member's primary source(s) —
// `.c/.cpp/.cc/.cxx/.S/.s/.asm` yield 1; `.rl6` yields 1 or 2 (source
// ± companion `.h`).
type sourceEmit struct {
	Ref          NodeRef
	OutPath      VFS
	CcIns        []VFS
	PrimaryCount int
}

func emitOneSource(ctx *genCtx, instance ModuleInstance, srcDir *string, srcRel string, in ModuleCCInputs, ancestorRebase bool) *sourceEmit {
	if isHeaderSource(srcRel) {
		return nil
	}

	// SRCDIR rebase fires only for the "include-from-parent" pattern
	// (PROGRAM whose SRCDIR is an ancestor of instance.Path; ragel6/bin
	// is canonical). LIBRARYs with SRCDIR (libcxxabi-parts, musl_extra,
	// tcmalloc/no_percpu_cache) keep srcInstance.Path == instance.Path
	// and resolve per-source inside EmitCC via composeCCPaths.
	srcInstance := instance

	if ancestorRebase {
		srcInstance.Path = *srcDir
	}

	// When instance is rebased (ragel6/bin), clear SrcDir so the
	// composer doesn't double-apply SRCDIR routing. When not rebased
	// (LIBRARY shape), keep SrcDir for per-source local-vs-SRCDIR
	// resolution.
	srcIn := in
	if ancestorRebase {
		srcIn.SrcDir = nil
	}

	switch {
	case strings.HasSuffix(srcRel, ".proto"):
		return emitLibraryProtoSource(ctx, srcInstance, srcDir, srcRel, srcIn)
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"):
		// Resolve transitive include closure for non-generated
		// sources. Generated sources (JS/R6 branches below) skip the
		// scanner — their primary input lives under $(B) and isn't on
		// disk at scan time.
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)

		// runtime_py3 __res.cpp / sitecustomize.cpp carry matching
		// .pyc.inc + PY_SRCS python inputs in REF. Lift extras BEFORE
		// resolving codegen dep refs so the .pyc.inc producer (AR
		// node) is reachable via IncludeInputs probe.
		extras := runtimePy3CCExtraInputs(srcInstance.Path, srcRel)
		if len(extras) > 0 {
			srcIn.IncludeInputs = appendVFSUnique(srcIn.IncludeInputs, extras)
		}
		// Thread codegen producer dep refs into the CC node. PR-M3-L0-codegen-
		// deps-EV-PB extended this to PB/EV (with platform keying) on top of
		// the EN path established by PR-M3-module-tag-and-stats-enums-dep.
		srcIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, srcIn.IncludeInputs)

		ref, outPath := EmitCC(srcInstance, srcRel, srcIn, ctx.host, ctx.emit)

		// AR/LD aggregate the per-CC inputs (primary source +
		// resolved headers) into their own inputs slice per sg.json
		// shape. Compose the input list here, matching what EmitCC
		// does internally.
		inputPath := emittedSourceInputPath(srcInstance, srcRel, srcIn, ctx.sourceRoot)
		ccInputs := append([]VFS{inputPath}, srcIn.IncludeInputs...)

		return &sourceEmit{Ref: ref, OutPath: outPath, CcIns: ccInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		// For x86_64 `.asm` sources the AS node carries yasm as
		// `ForeignDepRefs["tool"]` (reference uses yasm for every
		// `.asm` host source: util/system/context_x86.asm +
		// asmlib's 25 nodes). Other `.S` sources (target-side AS,
		// host chkstk, libcxx/libcxxabi shims) pass nil — they
		// assemble via clang's built-in assembler.
		var yasmRef *NodeRef

		if instance.Platform.ISA == ISAX8664 && strings.HasSuffix(srcRel, ".asm") {
			const yasmPath = "contrib/tools/yasm"

			yasmInstance := NewToolInstance(ctx.host, yasmPath)
			yasmInstance.Flags = inferFlagsFromPath(yasmPath, true)

			yasmResult := genModule(ctx, yasmInstance)
			ldRef := yasmResult.LDRef
			yasmRef = &ldRef
		}

		// Scan transitive headers for AS sources — a subset of `.S`
		// includes `.h`/`.inc` (e.g. cxxsupp/builtins/chkstk.S →
		// assembly.h + int_endianness.h). Populates the AS node's
		// inputs and feeds the downstream AR's memberInputs.
		asIn := srcIn
		asIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		// Thread full ModuleCCInputs into EmitAS so it composes
		// own/peer ADDINCL, own CFLAGS, and auto peer CFLAGS at the
		// same slots CC consumes them.
		ref, outPath := EmitAS(srcInstance, srcRel, asIn, yasmRef, ctx.host, ctx.emit)

		// SRCDIR routing for `.S` AR memberInput: when SRCDIR is set
		// and the source doesn't exist locally, resolve under SRCDIR
		// (same rule as composeASPaths' SRCDIR routing — as.go:316-336).
		// Empirical: tcmalloc/no_percpu_cache's
		// `tcmalloc/internal/percpu_rseq_asm.S` resolves at
		// `contrib/libs/tcmalloc/tcmalloc/internal/percpu_rseq_asm.S`.
		asInputRel := srcInstance.Path + "/" + srcRel

		if srcDir != nil && *srcDir != srcInstance.Path && !sourceExistsLocally(ctx.sourceRoot, srcInstance.Path, srcRel) {
			asInputRel = *srcDir + "/" + srcRel
		}

		// Collapse `..` segments so openssl's `crypto/../asm/...`
		// resolves to `asm/...` in AR memberInputs. AS node's own
		// input path is cleaned inside as.go.
		asInputRel = path.Clean(asInputRel)

		asInputs := append([]VFS{Source(asInputRel)}, asIn.IncludeInputs...)

		return &sourceEmit{Ref: ref, OutPath: outPath, CcIns: asInputs, PrimaryCount: 1}
	case strings.HasSuffix(srcRel, ".rl6"):
		// Host-ragel6 recursion: the resulting LD's outputs[0] is
		// threaded into EmitR6's cmd_args[0] for internal consistency
		// between the R6 invocation path and our own host LD.
		// `contrib/tools/ragel6/bin` is the real host-PROGRAM
		// directory; the parent ya.make uses INCLUDE(${ARCADIA_ROOT}/
		// ...) which our parser does not yet expand.
		const ragelBinPath = "contrib/tools/ragel6/bin"

		// Fallback ragel6 path for when the host walk fails its
		// parse. Literal matches the reference's invocation path so
		// the zero-host-LD codepath produces a meaningful argv.
		var ragelFallbackPath = Build("contrib/tools/ragel6/ragel6")

		var (
			ragelLDRef     NodeRef
			ragelBinaryVFS = ragelFallbackPath
		)

		ragelInstance := NewToolInstance(ctx.host, ragelBinPath)
		ragelInstance.Flags = inferFlagsFromPath(ragelInstance.Path, true)

		if exc := Try(func() {
			ragelResult := genModule(ctx, ragelInstance)
			ragelLDRef = ragelResult.LDRef
			if ragelResult.LDPath != nil {
				ragelBinaryVFS = *ragelResult.LDPath
			}
		}); exc != nil {
			// Only swallow *ParseError — the documented gap for
			// INCLUDE(${ARCADIA_ROOT}/...) the parser can't expand.
			// Other exceptions propagate.
			var pe *ParseError

			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}

			// Leave ragelLDRef zero-valued and ragelBinaryStr at
			// the fallback; R6 won't dep-link to a host ragel6 but
			// cmd_args[0] still names a plausible binary path.
		}

		// Scan `.rl6` transitive #include closure (the `.rl6` body
		// embeds `#include` directives resolving through the same
		// search-path / sysincl rules as `.cpp`/`.S`). The R6 node and
		// the downstream CC of the generated `.cpp` carry the same
		// closure (reference: util/datetime/parser.rl6 → 1009 inputs
		// each, identical at positions 1..).
		rl6Closure := walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)

		// REF's R6 closure resolves `#include <..._serialized.h>` via
		// stats.h for descendant headers (util/generic/
		// serialized_enum.h) but does NOT add the generated EN
		// `_serialized.{cpp,h}` siblings to R6 inputs. Strip both at
		// the R6 input boundary; descendant util headers reach R6 via
		// the same EmitsIncludes traversal regardless.
		rl6Closure = filterEnSerializedSiblings(rl6Closure)

		r6Ref, r6Out := EmitR6(srcInstance, srcRel, ragelLDRef, ragelBinaryVFS, srcIn.Ragel6Flags, rl6Closure, ctx.emit)

		// Register the R6 output (.rl6.cpp). Its parser-local payload
		// is the source .rl6's `h+cpp` bucket, which for Ragel is a
		// direct anchor back to the source file. That preserves
		// source-relative resolution and keeps the original .rl6 in the
		// downstream closure.
		rl6SourceVFS := Source(srcInstance.Path + "/" + srcRel)
		registerGeneratedParsedOutput(ctx, srcInstance, "R6", r6Out, remapSourceParsedIncludesToLocal(ctx, srcInstance, rl6SourceVFS, parsedIncludesHCPP))

		// Pass IsGenerated so the downstream CC composes inputPath
		// under $(B)/<srcInstance.Path>/<rel>; thread r6Ref as
		// Generator so the CC node carries an explicit dep on its R6
		// source-generator. Dispatch through the unified VFS-path
		// entry — the generated `.cpp` points back to the source via
		// the parser-layer h+cpp anchor.
		ccSrcRel := strings.TrimPrefix(r6Out.Rel, srcInstance.Path+"/")
		ccIncludeInputs := walkClosure(ctx, srcInstance, r6Out, srcIn)

		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.Generator = r6Ref
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = ccIncludeInputs
		// ymake's _LANG_CFLAGS_RL=-Wno-implicit-fallthrough applies
		// to CC compiles whose source is a .rl6-generated .cpp
		// (build/ymake.core.conf).
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		// Thread EN/PB/EV producer refs reached via the .rl6.cpp's
		// transitive include closure. Generator (r6Ref) is filtered
		// out so EmitCC's leading-DepRefs slot isn't duplicated.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r6Ref)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.host, ctx.emit)

		// AR/LD member-inputs aggregator excludes the BUILD_ROOT-
		// staged generated cpp (mirror of JS rule); carries the
		// original `.rl6` plus its companion `.h` header. Reference:
		// util's libyutil.a inputs include `parser.rl6` and
		// `parser.h`, never `parser.rl6.cpp`. Companion `.h` added
		// only when a same-basename `.h` sibling exists on disk.
		ccInputs := []VFS{rl6SourceVFS}
		primaryCount := 1

		companionRel := strings.TrimSuffix(srcRel, ".rl6") + ".h"
		companionAbs := filepath.Join(ctx.sourceRoot, srcInstance.Path, companionRel)

		if info, err := os.Stat(companionAbs); err == nil && !info.IsDir() {
			ccInputs = append(ccInputs, Source(srcInstance.Path+"/"+companionRel))
			primaryCount = 2
		}

		// Roll up the downstream CC's transitive header closure into
		// memberInputs so the AR/LD aggregator carries the libcxx /
		// musl / protobuf headers the generated .rl6.cpp includes.
		// Upstream ymake does this via EDT_BuildFrom
		// (json_visitor.cpp:788-789 NeedToPassInputs).
		ccInputs = append(ccInputs, ccIncludeInputs...)

		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: ccInputs, PrimaryCount: primaryCount}

	case strings.HasSuffix(srcRel, ".y"):
		return emitBisonY(ctx, srcInstance, srcRel, srcIn, srcIn.BisonGenExt)

	case strings.HasSuffix(srcRel, ".ev"):
		// `.ev` sources in a LIBRARY module (e.g. devtools/ymake/diag/
		// trace.ev). Emits one EV node (generating .ev.pb.cc +
		// .ev.pb.h) then a downstream CC node compiling .ev.pb.cc.
		{
			evSource := resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir)
			evRelPath := evSource.Rel

			// Walk host tool programs.
			cppStyleguideBinary := pbCppStyleguideVFS
			protocBinary := pbProtocBinaryVFS
			event2cppBinary := evEvent2cppBinaryVFS

			var cppStyleguideLDRef, protocLDRef, event2cppLDRef NodeRef

			protocHostInst := NewToolInstance(ctx.host, pbProtocModule)
			protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

			if exc := Try(func() {
				result := genModule(ctx, protocHostInst)
				protocLDRef = result.LDRef
				if result.LDPath != nil {
					protocBinary = *result.LDPath
				}
			}); exc != nil {
				_ = exc
			}

			cppStyleguideHostInst := NewToolInstance(ctx.host, pbCppStyleguideModule)
			cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

			if exc := Try(func() {
				result := genModule(ctx, cppStyleguideHostInst)
				cppStyleguideLDRef = result.LDRef
				if result.LDPath != nil {
					cppStyleguideBinary = *result.LDPath
				}
			}); exc != nil {
				_ = exc
			}

			event2cppHostInst := NewToolInstance(ctx.host, evEvent2cppModule)
			event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

			if exc := Try(func() {
				result := genModule(ctx, event2cppHostInst)
				event2cppLDRef = result.LDRef
				if result.LDPath != nil {
					event2cppBinary = *result.LDPath
				}
			}); exc != nil {
				_ = exc
			}

			// moduleTag is empty for LIBRARY modules (no "cpp_proto" tag).
			evRef := EmitEV(
				srcInstance, evRelPath,
				cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				nil, ctx.sourceRoot, ctx.emit)

			// Register .ev.pb.h with EmitsIncludes from .ev imports
			// plus protobuf runtime headers.
			evH := Build(evRelPath + ".pb.h")
			evPbCC := Build(evRelPath + ".pb.cc")

			// Stash the EV NodeRef under both outputs on the emitting
			// platform so consumer CCs in other modules whose
			// IncludeInputs include this .ev.pb.h / .ev.pb.cc dep on
			// the producer.
			evKey := codegenOutputKey{platform: srcInstance.Platform}
			evKey.path = evH
			ctx.evOutputs[evKey] = evRef
			evKey.path = evPbCC
			ctx.evOutputs[evKey] = evRef
			if reg := codegenRegForInstance(ctx, srcInstance); reg != nil {
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath, "")
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
				evEmitsIncludes := make([]VFS, 0, len(directImports)+len(protobufRuntimeHeaders)+len(evExtras))
				evEmitsIncludes = append(evEmitsIncludes, directImports...)
				evEmitsIncludes = append(evEmitsIncludes, protobufRuntimeHeaders...)
				evEmitsIncludes = append(evEmitsIncludes, evExtras...)
				registerGeneratedOutput(ctx, srcInstance, "EV", evH, evEmitsIncludes)
				// Register the .ev.pb.cc output. event2cpp emits a
				// `#include "<base>.ev.pb.h"` plus protobuf runtime
				// headers. Mirror the .pb.h list for symmetry with PB.
				registerGeneratedOutput(ctx, srcInstance, "EV", evPbCC, append([]VFS{evH}, protobufRuntimeHeaders...))
			}

			// Emit downstream CC for the generated .ev.pb.cc via the
			// unified VFS-path entry — the .ev.pb.cc is registered
			// above with the right EmitsIncludes; WalkClosure walks
			// into .pb.h and the protobuf runtime headers.
			evPbCCSuffix := srcRel + ".pb.cc"
			ccIn := srcIn
			ccIn.IsGenerated = true
			ccIn.Generator = evRef
			ccIn.HasGenerator = true
			ccIn.IncludeInputs = walkClosure(ctx, srcInstance, evPbCC, srcIn)
			// .ev.pb.cc.o consumer must not carry its OWN .ev.pb.h in
			// inputs[] (REF omits the self-include; cross-imported
			// sibling .ev.pb.h entries remain). The walker reaches
			// evH transitively because the .pb.cc is registered with
			// evH as its first EmitsIncludes child — drop just that.
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
			// protoc emits `#include "google/protobuf/wire_format.h"`
			// directly. Add to inputs only on this CC node (not via
			// registry — that would over-emit).
			ccIn.IncludeInputs = append(ccIn.IncludeInputs, wireFormatVFS)
			// Thread cross-codegen producer refs (e.g. an .ev that
			// imports another module's .proto pulls the peer's PB
			// into the consumer CC's deps via its .pb.h in inputs[]).
			ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, evRef)

			ref, outPath := EmitCC(srcInstance, evPbCCSuffix, ccIn, ctx.host, ctx.emit)

			// Primary input for AR/LD memberInputs is the original
			// .ev source; wire_format.h also propagates into the AR
			// rollup (matched in REF on libdevtools-ymake-diag.a).
			return &sourceEmit{
				Ref:          ref,
				OutPath:      outPath,
				CcIns:        []VFS{Source(srcInstance.Path + "/" + srcRel), wireFormatVFS},
				PrimaryCount: 1,
			}
		}

	case strings.HasSuffix(srcRel, ".rl"):
		// ragel5 two-step code generation (.rl → .rl.tmp → .rl5.cpp).
		// Mirrors the .rl6 branch: walk the two host ragel5 PROGRAMs,
		// emit R5, then emit a CC for the generated .rl5.cpp.
		const (
			ragel5Path  = "contrib/tools/ragel5/ragel"
			rlgenCdPath = "contrib/tools/ragel5/rlgen-cd"
		)
		var (
			ragel5Fallback  = Build("contrib/tools/ragel5/ragel/ragel5")
			rlgenCdFallback = Build("contrib/tools/ragel5/rlgen-cd/rlgen-cd")
		)

		var (
			ragel5LDRef   NodeRef
			rlgenCdLDRef  NodeRef
			ragel5BinVFS  = ragel5Fallback
			rlgenCdBinVFS = rlgenCdFallback
		)

		ragel5Instance := NewToolInstance(ctx.host, ragel5Path)
		ragel5Instance.Flags = inferFlagsFromPath(ragel5Path, true)

		if exc := Try(func() {
			res := genModule(ctx, ragel5Instance)
			ragel5LDRef = res.LDRef
			if res.LDPath != nil {
				ragel5BinVFS = *res.LDPath
			}
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}

		rlgenCdInstance := NewToolInstance(ctx.host, rlgenCdPath)
		rlgenCdInstance.Flags = inferFlagsFromPath(rlgenCdPath, true)

		if exc := Try(func() {
			res := genModule(ctx, rlgenCdInstance)
			rlgenCdLDRef = res.LDRef
			if res.LDPath != nil {
				rlgenCdBinVFS = *res.LDPath
			}
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}

		r5Ref, r5TmpOut, r5CppOut := EmitR5(srcInstance, srcRel, ragel5LDRef, rlgenCdLDRef, ragel5BinVFS, rlgenCdBinVFS, ctx.emit)
		_ = r5Ref

		// Register R5 outputs. The generated .rl5.cpp inherits the
		// source .rl's `h+cpp` bucket, which for Ragel is a direct
		// parser-layer anchor back to the source file. That preserves
		// source-relative resolution and keeps the original .rl in the
		// downstream closure. The .tmp intermediate has no
		// consumer-visible includes. ProducerRef = r5Ref so the
		// downstream CC threads R5 into its deps[].
		rlSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		registerBoundGeneratedOutput(ctx, srcInstance, "R5", r5TmpOut, nil, r5Ref)
		registerBoundGeneratedParsedOutput(ctx, srcInstance, "R5", r5CppOut, remapSourceParsedIncludesToLocal(ctx, srcInstance, rlSourceVFS, parsedIncludesHCPP), r5Ref)

		// Downstream CC for the generated .rl5.cpp via the unified
		// VFS-path entry — the .rl5.cpp is registered above with the
		// parser-layer h+cpp anchor back to the source `.rl`.
		ccSrcRel := strings.TrimPrefix(r5CppOut.Rel, srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IsGenerated = true
		ccClosure := walkClosure(ctx, srcInstance, r5CppOut, srcIn)
		// ragel5 emits two outputs (.rl.tmp + .rl5.cpp); upstream
		// ymake lists BOTH in the downstream CC's inputs[]. walkClosure
		// scans only the .rl5.cpp, so inject .rl.tmp explicitly
		// (prepended to keep it adjacent to the primary in DFS order).
		// The .rl.tmp does NOT propagate into AR/LD memberInputs
		// (sg2.json shows only the .rl source rolls up).
		ccIn.IncludeInputs = append([]VFS{r5TmpOut}, ccClosure...)
		ccIn.PerSourceCFlags = append(append([]string(nil), srcIn.PerSourceCFlags...), "-Wno-implicit-fallthrough")
		// Thread codegen producer refs via the .rl5.cpp's transitive
		// include closure. Prepend r5Ref because WalkClosure skips
		// the root (r5CppOut) so the registry probe alone wouldn't
		// surface R5; REF's R5-derived CC carries R5 as leading dep.
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, r5Ref)
		ccIn.ExtraDepRefs = append([]NodeRef{r5Ref}, ccIn.ExtraDepRefs...)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.host, ctx.emit)

		// AR/LD member inputs: original .rl source plus the downstream
		// CC's transitive header closure (upstream ymake propagates
		// via EDT_BuildFrom — json_visitor.cpp:788-789 NeedToPassInputs).
		// Uses ccClosure (NOT ccIn.IncludeInputs) so the .rl.tmp
		// sibling stays scoped to the CC consumer.
		rlMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, ccClosure...)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: rlMemberInputs, PrimaryCount: 1}

	case strings.HasSuffix(srcRel, ".h.in"):
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		cfRef, cfOut := EmitCF(srcInstance, srcRel, srcIn, ctx.emit)

		inSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		registerBoundGeneratedOutput(ctx, srcInstance, "CF", cfOut, []VFS{inSourceVFS, configureFilePyVFS}, cfRef)

		cfMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, srcIn.IncludeInputs...)

		return &sourceEmit{Ref: cfRef, OutPath: cfOut, CcIns: cfMemberInputs, PrimaryCount: 1}

	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		// CONFIGURE_FILE template. Emit a CF node running
		// configure_file.py to expand @VAR@ placeholders, then a CC
		// for the generated .cpp / .c. cfg vars come via
		// srcIn.DefaultVars (set by genModule); BUILD_TYPE=DEBUG is
		// hardcoded. Output strips .in suffix
		// (sandbox.cpp.in → sandbox.cpp). Scan the .in template's
		// transitive include closure and fold into IncludeInputs
		// before EmitCF (sandbox.cpp.in → 795-entry closure;
		// build_info.cpp.in → 5).
		srcIn.IncludeInputs = walkClosure(ctx, srcInstance, resolveSourceVFS(ctx, srcInstance, srcRel, srcIn.SrcDir), srcIn)
		cfRef, cfOut := EmitCF(srcInstance, srcRel, srcIn, ctx.emit)

		// Register CF output. configure_file.py performs `@VAR@`
		// substitution but leaves `#include` directives intact, so
		// the generated .cpp's direct includes are the .cpp.in's.
		// Register the .cpp.in as the single EmitsIncludes child so
		// WalkClosure recurses via the FS locator. configure_file.py
		// is wired as an input on every CC consumer of the generated
		// .cpp (verified on build_info.cpp.o and sandbox.cpp.o).
		// ProducerRef = cfRef so downstream resolveCodegenDepRefs
		// threads the CF producer into deps[].
		inSourceVFS := Source(srcInstance.Path + "/" + srcRel)
		registerBoundGeneratedOutput(ctx, srcInstance, "CF", cfOut, []VFS{inSourceVFS, configureFilePyVFS}, cfRef)

		// Downstream CC for the generated .cpp / .c via the unified
		// VFS-path entry — the .cpp is registered with the .cpp.in as
		// its direct include; WalkClosure recurses via the FS locator.
		ccSrcRel := strings.TrimPrefix(cfOut.Rel, srcInstance.Path+"/")
		ccIn := srcIn
		ccIn.IsGenerated = true
		ccIn.IncludeInputs = walkClosure(ctx, srcInstance, cfOut, srcIn)
		// Thread codegen producer refs via the CF-generated .cpp's
		// transitive include closure. Prepend cfRef because
		// WalkClosure skips the root (cfOut) so registry probe alone
		// wouldn't find it; REF's CF-derived CC carries the CF
		// producer as leading dep (sandbox.cpp.o → CF sandbox.cpp).
		ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, srcInstance, ccIn.IncludeInputs, cfRef)
		ccIn.ExtraDepRefs = append([]NodeRef{cfRef}, ccIn.ExtraDepRefs...)

		ccRef, ccOut := EmitCC(srcInstance, ccSrcRel, ccIn, ctx.host, ctx.emit)

		// AR/LD member inputs: original .cpp.in / .c.in source plus
		// the downstream CC's transitive header closure (matching
		// upstream ymake's EDT_BuildFrom propagation —
		// json_visitor.cpp:788-789 NeedToPassInputs).
		cfMemberInputs := append([]VFS{Source(srcInstance.Path + "/" + srcRel)}, ccIn.IncludeInputs...)
		return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: cfMemberInputs, PrimaryCount: 1}
	}

	// Known-deferred source kinds are silently skipped rather than
	// throwing. Returning nil means the source contributes nothing to
	// the AR/LD node set; the module may become header-only if all
	// its sources are deferred.
	if isSkippedSource(srcRel) {
		return nil
	}

	ThrowFmt("gen: %s: unsupported source extension in %q", instance.Path, srcRel)

	return nil
}

func emitLibraryProtoSource(ctx *genCtx, instance ModuleInstance, srcDir *string, srcRel string, in ModuleCCInputs) *sourceEmit {
	cppStyleguideBinary := pbCppStyleguideVFS
	protocBinary := pbProtocBinaryVFS

	var cppStyleguideLDRef, protocLDRef NodeRef

	protocHostInst := NewToolInstance(ctx.host, pbProtocModule)
	protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)
	if exc := Try(func() {
		result := genModule(ctx, protocHostInst)
		protocLDRef = result.LDRef
		if result.LDPath != nil {
			protocBinary = *result.LDPath
		}
	}); exc != nil {
		_ = exc
	}

	cppStyleguideHostInst := NewToolInstance(ctx.host, pbCppStyleguideModule)
	cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)
	if exc := Try(func() {
		result := genModule(ctx, cppStyleguideHostInst)
		cppStyleguideLDRef = result.LDRef
		if result.LDPath != nil {
			cppStyleguideBinary = *result.LDPath
		}
	}); exc != nil {
		_ = exc
	}

	protoRelPath := protoSourceRelPath(ctx.sourceRoot, instance, &moduleData{srcDir: srcDir}, srcRel)
	pbRef := EmitPB(
		instance, protoRelPath, cppStyleguideLDRef, protocLDRef,
		NodeRef{}, cppStyleguideBinary, protocBinary, pbGrpcCppVFS,
		false, nil, "", false, ctx.sourceRoot, ctx.emit,
	)

	protoBase := strings.TrimSuffix(protoRelPath, ".proto")
	pbH := Build(protoBase + ".pb.h")
	pbCC := Build(protoBase + ".pb.cc")

	pbKey := codegenOutputKey{platform: instance.Platform}
	pbKey.path = pbH
	ctx.pbOutputs[pbKey] = pbRef
	pbKey.path = pbCC
	ctx.pbOutputs[pbKey] = pbRef

	if reg := codegenRegForInstance(ctx, instance); reg != nil {
		directImports := protoDirectImportIncludes(ctx.sourceRoot, protoRelPath, "")
		extras := pbDescriptorImporterExtras(ctx.sourceRoot, protoRelPath)
		emitsIncludes := make([]VFS, 0, len(directImports)+len(protobufRuntimeHeaders)+len(extras))
		emitsIncludes = append(emitsIncludes, directImports...)
		emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
		emitsIncludes = append(emitsIncludes, extras...)
		registerGeneratedOutput(ctx, instance, "PB", pbH, emitsIncludes)

		pbCCEmits := make([]VFS, 0, 3+len(protobufRuntimeHeaders)+len(pbCcDeepRuntimeHeaders))
		pbCCEmits = append(pbCCEmits, pbH)
		pbCCEmits = append(pbCCEmits, Source(protoRelPath))
		pbCCEmits = append(pbCCEmits, pbWrapperVFS)
		pbCCEmits = append(pbCCEmits, protobufRuntimeHeaders...)
		pbCCEmits = append(pbCCEmits, pbCcDeepRuntimeHeaders...)
		registerGeneratedOutput(ctx, instance, "PB", pbCC, pbCCEmits)
	}

	ccIn := in
	ccIn.IsGenerated = true
	ccIn.Generator = pbRef
	ccIn.HasGenerator = true
	ccIn.IncludeInputs = walkClosure(ctx, instance, pbCC, in)
	ccIn.ExtraDepRefs = resolveCodegenDepRefs(ctx, instance, ccIn.IncludeInputs, pbRef)

	ccSrcRel := strings.TrimPrefix(protoBase+".pb.cc", instance.Path+"/")
	ccRef, ccOut := EmitCC(instance, ccSrcRel, ccIn, ctx.host, ctx.emit)
	ccInputs := make([]VFS, 0, 1+len(ccIn.IncludeInputs))
	ccInputs = append(ccInputs, Source(protoRelPath))
	ccInputs = append(ccInputs, ccIn.IncludeInputs...)

	return &sourceEmit{Ref: ccRef, OutPath: ccOut, CcIns: ccInputs, PrimaryCount: 1}
}

// emittedSourceInputPath mirrors composeCCPaths' inputPath logic so
// the walker composes the AR/LD inputs aggregator without round-
// tripping through the emitted node. Returns `$(S)/...` (or
// `$(B)/...` for IsGenerated).
func emittedSourceInputPath(instance ModuleInstance, srcRel string, in ModuleCCInputs, sourceRoot string) VFS {
	if in.IsGenerated {
		return Build(instance.Path + "/" + srcRel)
	}

	if in.SrcDir != nil && *in.SrcDir != instance.Path {
		localCandidate := filepath.Join(sourceRoot, instance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			return Source(*in.SrcDir + "/" + srcRel)
		}
	}

	return Source(instance.Path + "/" + srcRel)
}

// joinSrcsIncludeClosure walks the include graph for a JOIN_SRCS
// member set. DFS runs over all members with a SHARED visited set
// (mirroring the joined compile — headers reached once stay deduped),
// so total work is O(union closure) not O(sum per-source).
//
// `scanPlatform` chooses scanner + arch search-paths: callers pass
// `srcInstance.Platform` normally; the JS-target override passes
// `ctx.target` so the closure resolves against target-arch musl even
// when the surrounding walk is host-axis. instance.Platform is read
// for module-level facts (Path, Flags.NoStdInc), NOT mutated.
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

		if in.SrcDir != nil && *in.SrcDir != srcInstance.Path {
			localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, src)
			info, err := os.Stat(localCandidate)

			if err != nil || info.IsDir() {
				srcRelOnDisk = *in.SrcDir + "/" + src
			}
		}

		cfg := ScanContext{
			SourceRel:       srcRelOnDisk,
			OwnAddIncl:      in.AddIncl,
			PeerAddInclSet:  in.PeerAddInclGlobal,
			BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.NoStdInc, scanPlatform),
		}

		// scanCtx dispatch via genCtx.getScanCtx — one scanCtx per
		// unique ctxHash. Within this loop every source's cfg differs
		// only in SourceRel; routing through getScanCtx lets
		// resolveCache / subgraphCache entries from earlier sources
		// serve later sources at the same ctxHash.
		sc := ctx.getScanCtx(scanner, cfg)

		// dfs reads sc.cfg.SourceRel for srcClassHash; set it here
		// before invoking against the shared visited+order so
		// sysinclSourceLookup keys on the right path.
		sc.cfg.SourceRel = srcRelOnDisk

		// Scanner walks operate on VFS values; the FS translation
		// happens at scanDirectives / fileExists.
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

func appendVFSUnique(dst []VFS, src []VFS) []VFS {
	seen := make(map[VFS]struct{}, len(dst)+len(src))

	for _, v := range dst {
		seen[v] = struct{}{}
	}

	for _, v := range src {
		if _, dup := seen[v]; dup {
			continue
		}

		seen[v] = struct{}{}
		dst = append(dst, v)
	}

	return dst
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

// jsTargetPeerAddIncl rebases a host-axis PeerAddInclGlobal slice to
// the target-axis musl-arch layout for the JS-node closure scan. JS
// nodes are anchored to the target platform axis, so their include
// closure reflects the target's musl-arch paths.
//
// Narrow shim — only the musl-arch entry is rewritten; other entries
// pass through. TODO: replace with general target-addincl propagation.
func jsTargetPeerAddIncl(hostPeerAddIncl []VFS, from, to ISA) []VFS {
	fromMuslArch := Source("contrib/libs/musl/arch/" + string(from))
	toMuslArch := Source("contrib/libs/musl/arch/" + string(to))

	out := make([]VFS, len(hostPeerAddIncl))

	for i, p := range hostPeerAddIncl {
		if p == fromMuslArch {
			out[i] = toMuslArch
		} else {
			out[i] = p
		}
	}

	return out
}

// resolveSourceVFS composes the `$(S)/...` VFS path of a SRCS-declared
// source with composeCCPaths' SRCDIR-aware fallback: when SRCDIR is set
// and no local file exists at instance.Path/<srcRel>, resolve under
// SRCDIR. Registration-time resolution; os.Stat is legitimate here
// because it feeds path composition, not scanner-internal dispatch.
func resolveSourceVFS(ctx *genCtx, srcInstance ModuleInstance, srcRel string, srcDir *string) VFS {
	srcRelOnDisk := srcInstance.Path + "/" + srcRel

	if srcDir != nil && filepath.Clean(*srcDir) != "." && filepath.Clean(*srcDir) != srcInstance.Path {
		cleanSrcDir := filepath.Clean(*srcDir)
		localCandidate := filepath.Join(ctx.sourceRoot, srcInstance.Path, srcRel)
		info, err := os.Stat(localCandidate)

		if err != nil || info.IsDir() {
			srcRelOnDisk = cleanSrcDir + "/" + srcRel
		}
	}

	return Source(srcRelOnDisk)
}

// resolveCodegenDepRefs replaced by the EN/PB/EV-aware version at line 344
// (PR-M3-L0-codegen-deps-EV-PB).

// walkClosure resolves the transitive include closure of a source
// rooted at any VFS path — `$(S)/...` for FS-resident sources or
// `$(B)/...` for codegen outputs registered in the CodegenRegistry.
// Scanner's locator dispatches FS-vs-codegen internally. ScanContext
// mirrors cmd_args -I: own AddIncl + peer GLOBAL AddIncl + cc-bundle
// implicit baseline (linux-headers + active musl-arch).
func walkClosure(ctx *genCtx, srcInstance ModuleInstance, vfsPath VFS, in ModuleCCInputs) []VFS {
	scanner := ctx.scannerFor(srcInstance)
	if scanner == nil {
		return nil
	}

	// SourceRel feeds srcClassHash (per-source subgraph-cache key).
	// WalkClosure overwrites it per-call for SOURCE_ROOT paths;
	// BUILD_ROOT paths leave it as set and never consult it.
	cfg := ScanContext{
		SourceRel:       vfsPath.Rel,
		OwnAddIncl:      in.AddIncl,
		PeerAddInclSet:  in.PeerAddInclGlobal,
		BaseSearchPaths: includeScannerBasePaths(srcInstance.Flags.NoStdInc, srcInstance.Platform),
	}

	sc := ctx.getScanCtx(scanner, cfg)

	return sc.WalkClosure(vfsPath)
}

// includeScannerBasePaths returns the implicit include search path
// the cc bundle adds via cmd_args (SOURCE_ROOT + linux-headers +
// musl-arch when applicable). Used as fallback resolution candidates
// so `<util/folder/path.h>` and `<linux/types.h>` resolve compiler-
// identically.
//
// Non-musl flavours prepend an empty-string entry representing
// SOURCE_ROOT itself (mirrors `-I$(S)`); `<util/foo.h>` tries
// $(S)/util/foo.h before linux-headers.
//
// Musl flavours MUST NOT get the empty prefix — `-nostdinc` plus a
// fully explicit muslCcIncludes search path. Adding SOURCE_ROOT
// would cause false resolution of system-form includes against the
// repo root, silently expanding musl CC input sets.
//
// `libcMusl` is the per-MODULE flag; `scanPlatform` is the platform
// to resolve against (typically instance.Platform, but JOIN_SRCS
// during a host walk passes ctx.target to force target-arch paths).
func includeScannerBasePaths(libcMusl bool, scanPlatform *Platform) []VFS {
	base := []VFS{
		Source("contrib/libs/linux-headers"),
		Source("contrib/libs/linux-headers/_nf"),
	}

	if libcMusl {
		// Mirror muslCcIncludes / muslCcIncludesX8664: arch + generic
		// + src/include + src/internal + include + extra.
		muslPaths := []VFS{
			Source("contrib/libs/musl/arch/" + string(scanPlatform.ISA)),
			Source("contrib/libs/musl/arch/generic"),
			Source("contrib/libs/musl/src/include"),
			Source("contrib/libs/musl/src/internal"),
			Source("contrib/libs/musl/include"),
			Source("contrib/libs/musl/extra"),
		}

		// Musl paths come BEFORE linux-headers in the cmd_args ordering.
		out := make([]VFS, 0, len(muslPaths)+len(base))
		out = append(out, muslPaths...)
		out = append(out, base...)

		return out
	}

	// Non-musl: prepend the empty-prefix entry (SOURCE_ROOT itself) so
	// repo-rooted system-form includes like `<util/folder/path.h>`
	// resolve against $(S)/util/folder/path.h.
	out := make([]VFS, 0, 1+len(base))
	out = append(out, Source(""))
	out = append(out, base...)

	return out
}
