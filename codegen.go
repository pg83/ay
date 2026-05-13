package main

// codegen.go — Python / Enum / Resource generator-driven nodes.
//
// emitPySrcs:    one PY (.yapyc3) node per PY_SRCS source.
// emitPyRegister: PY+CC pair per PY_REGISTER() declaration.
// emitEnumSrcs:  EN+downstream-CC chain per GENERATE_ENUM_SERIALIZATION.
//
// All three drive through EmitEN / EmitPB / EmitEV-style codegen
// producers and the matching CodegenRegistry registration; the
// downstream CC for each is composed via EmitCC with IsGenerated=true.

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func emitPySrcs(ctx *genCtx, instance ModuleInstance, d *moduleData) {
	if len(d.pySrcs) == 0 {
		return
	}

	// ENABLE(PYBUILD_NO_PYC) suppresses yapyc3 generation. Modules like
	// contrib/tools/python3/lib2/py declare all Python sources but set
	// this flag to prevent .pyc/.yapyc3 files from being emitted —
	// they embed the sources via RESOURCE/objcopy instead.
	if d.pyBuildNoPYC {
		return
	}

	// Walk tools/py3cc/bin and tools/py3cc/slow as HOST tools to get
	// their LD NodeRefs. Both are PROGRAM modules on x86_64.
	const (
		py3ccBinPath  = "tools/py3cc/bin"
		py3ccSlowPath = "tools/py3cc/slow"
	)

	// Canonical binary paths ($(B)-rooted) used in cmd_args
	// and inputs when the host walk succeeds or as fallbacks when it fails.
	var (
		py3ccBinaryCanonical     = Build("tools/py3cc/py3cc").String()
		py3ccSlowBinaryCanonical = Build("tools/py3cc/slow/py3cc").String()
	)

	var (
		py3ccLDRef     NodeRef
		py3ccSlowLDRef NodeRef
		py3ccBinary    = py3ccBinaryCanonical
		py3ccSlowBin   = py3ccSlowBinaryCanonical
	)

	// Walk tools/py3cc/bin (the main py3cc binary).
	py3ccHostInst := NewToolInstance(ctx.host, py3ccBinPath, instance.Language)
	py3ccHostInst.Flags = inferFlagsFromPath(py3ccBinPath, true)

	if exc := Try(func() {
		result := genModule(ctx, py3ccHostInst)
		py3ccLDRef = result.LDRef
		// canonicalizePy3ccBinaryPath rewrites
		// $(B)/tools/py3cc/bin/py3cc →
		// $(B)/tools/py3cc/py3cc to match the reference
		// yapyc3 cmd_args[0] shape. tools/py3cc/bin/ya.make declares
		// SRCDIR(tools/py3cc) so the upstream intent is a top-level
		// binary; we walk /bin/ as a stopgap (same pattern as ragel6).
		py3ccBinary = canonicalizePy3ccBinaryPath(result.LDPath)
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
		// Leave zero ref; py3ccBinary stays at canonical fallback.
	}

	// Walk tools/py3cc/slow (the slow-py3cc binary). tools/py3cc/slow/ya.make
	// uses IF(NOT PREBUILT) INCLUDE(bin/ya.make) which our parser expands
	// (PREBUILT=false). However tools/py3cc/slow/bin declares PY3_PROGRAM_BIN,
	// which isMultimoduleLibraryType routes to the header-only path, so
	// LDPath is empty. Only update py3ccSlowBin when the walk produces a
	// non-empty path; otherwise the canonical fallback
	// $(B)/tools/py3cc/slow/py3cc (pre-initialised above) is used.
	py3ccSlowHostInst := NewToolInstance(ctx.host, py3ccSlowPath, instance.Language)
	py3ccSlowHostInst.Flags = inferFlagsFromPath(py3ccSlowPath, true)

	if exc := Try(func() {
		result := genModule(ctx, py3ccSlowHostInst)
		py3ccSlowLDRef = result.LDRef
		if result.LDPath != "" {
			py3ccSlowBin = result.LDPath
		}
		// If LDPath is empty (PY3_PROGRAM_BIN → header-only stub),
		// py3ccSlowBin retains its canonical fallback value.
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
		// Leave zero ref; py3ccSlowBin stays at canonical fallback.
	}

	// PR-M3-F-1: walk tools/rescompiler/bin, tools/rescompressor/bin, and
	// tools/archiver as host tools. These are referenced by PY (objcopy) and
	// AR (pyc.inc) nodes in the M3 closure. ldBinaryDir lifts the output dirs.
	// Walks are eager (at most once per ctx due to memoization); LD NodeRefs
	// are not yet wired into the yapyc3 PY nodes emitted below (that wiring
	// is deferred to a later PR when the full objcopy PY emitter lands).
	const (
		rescompilerBinPath  = "tools/rescompiler/bin"
		rescompressorBinPath = "tools/rescompressor/bin"
		archiverPath        = "tools/archiver"
	)

	walkHostTool := func(path string) {
		hostInst := NewToolInstance(ctx.host, path, instance.Language)
		hostInst.Flags = inferFlagsFromPath(path, true)
		if exc := Try(func() {
			genModule(ctx, hostInst)
		}); exc != nil {
			var pe *ParseError
			if !errors.As(exc.AsError(), &pe) {
				panic(exc)
			}
		}
	}

	walkHostTool(rescompilerBinPath)
	walkHostTool(rescompressorBinPath)
	walkHostTool(archiverPath)

	// Emit one yapyc3 PY node per .py source.
	for _, srcRel := range d.pySrcs {
		srcAbs := Source(instance.Path + "/" + srcRel)

		// The "module name" arg: <modulePath>/<srcRel>- (trailing dash).
		moduleName := instance.Path + "/" + srcRel + "-"

		// Output suffix: flat → .py.yapyc3; subdir → .py.3kp2.yapyc3.
		var outputPath VFS
		if strings.Contains(srcRel, "/") {
			// The srcRel already ends in ".py"; insert ".3kp2" before ".yapyc3".
			outputPath = Build(instance.Path + "/" + srcRel + ".3kp2.yapyc3")
		} else {
			outputPath = Build(instance.Path + "/" + srcRel + ".yapyc3")
		}

		cmdArgs := []string{
			py3ccBinary,
			"--slow-py3cc",
			py3ccSlowBin,
			moduleName,
			srcAbs.String(),
			outputPath.String(),
		}

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
			"PYTHONHASHSEED":         "0",
		}

		node := &Node{
			Cmds: []Cmd{
				{
					CmdArgs: cmdArgs,
					Env:     env,
				},
			},
			Env:     env,
			Inputs:  []VFS{ParseVFSOrSource(py3ccBinary), ParseVFSOrSource(py3ccSlowBin), srcAbs},
			Outputs: []VFS{outputPath},
			KV: map[string]string{
				"p":  "PY",
				"pc": "yellow",
			},
			Tags: []string{},
			TargetProperties: func() map[string]string {
				tp := map[string]string{"module_dir": instance.Path}
				// PR-M3-module-tag-and-stats-enums-dep: PY23_LIBRARY's
				// .yapyc3 nodes carry `module_tag=py3` in REF (matches
				// MODULE_TAG=PY3 from _ARCADIA_PYTHON3_ADDINCL via the
				// PY3 submodule). PY3_LIBRARY / PY2_LIBRARY etc keep no
				// tag (the type is its own default and upstream omits
				// redundant properties).
				if d.moduleStmt.Name == "PY23_LIBRARY" {
					tp["module_tag"] = "py3"
				}
				return tp
			}(),
			Platform: string(instance.Platform.Target),
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
		}

		// Wire py3cc LD refs into both DepRefs (topology/deps) and
		// ForeignDepRefs["tool"] (foreign_deps.tool) to match the
		// reference yapyc3 node shape.  Only add non-zero refs (zero
		// ref means the host walk failed and we have no LD node to
		// reference).
		var toolRefs []NodeRef

		if py3ccLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, py3ccLDRef)
			toolRefs = append(toolRefs, py3ccLDRef)
		}

		if py3ccSlowLDRef != (NodeRef{}) {
			node.DepRefs = append(node.DepRefs, py3ccSlowLDRef)
			toolRefs = append(toolRefs, py3ccSlowLDRef)
		}

		if len(toolRefs) > 0 {
			node.ForeignDepRefs = map[string][]NodeRef{"tool": toolRefs}
		}

		pyRef := ctx.emit.Emit(node)

		// PR-M3-L0-cascade-close-v2: register the .yapyc3 output in the
		// codegen registry so the downstream objcopy CC's input-driven
		// resolveCodegenDepRefsExt lookup threads the PY producer into
		// its deps[]. Per Plan B PR-2: 41 PY-leaf objcopy_*.o files lack
		// the PY ref edge today — this closes them.
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:    "PY",
				OutputPath:     outputPath,
				ProducerRef:    pyRef,
				HasProducerRef: true,
			})
		}
	}
}

// genPy3RegScriptVFS is the source-relative VFS path to the
// gen_py3_reg.py script invoked by every PY_REGISTER's PY node
// (mirror of macro _PY3_REGISTER at build/ymake.core.conf:4086-4089).
var genPy3RegScriptVFS = Source("build/scripts/gen_py3_reg.py")
var genPy3RegScriptPath = genPy3RegScriptVFS.String()

// emitPyRegister emits the PY+CC pair for each PY_REGISTER(arg) entry in
// d.pyRegister. Each arg:
//   - one PY node:  python3 gen_py3_reg.py <arg> $(B)/<modPath>/<arg>.reg3.cpp
//   - one CC node:  compiles the generated `.reg3.cpp` into `.reg3.cpp.o` (or
//     `.reg3.cpp.py3.o` when py3Suffix is set).
//
// Both nodes' refs flow into globalRefs/globalOutputs (the upstream
// _PY3_REGISTER macro emits `SRCS(GLOBAL $Func.reg3.cpp)`, so the CC output
// archives in the module's `.global.a`).
//
// Mirror of macro _PY3_REGISTER at build/ymake.core.conf:4086-4089.
func emitPyRegister(ctx *genCtx, instance ModuleInstance, d *moduleData, in ModuleCCInputs, py3Suffix bool) (refs []NodeRef, outputs []VFS, memberInputs []VFS) {
	if len(d.pyRegister) == 0 {
		return nil, nil, nil
	}

	for _, arg := range d.pyRegister {
		regCpp := arg + ".reg3.cpp"
		regCppVFS := Build(instance.Path + "/" + regCpp)
		regCppAbs := regCppVFS.String()

		env := map[string]string{
			"ARCADIA_ROOT_DISTBUILD": "$(S)",
		}

		pyCmdArgs := []string{
			instance.Platform.Tools.Python3,
			genPy3RegScriptPath,
			arg,
			regCppAbs,
		}

		pyNode := &Node{
			Cmds: []Cmd{
				{CmdArgs: pyCmdArgs, Env: env},
			},
			Env:     env,
			Inputs:  []VFS{genPy3RegScriptVFS},
			Outputs: []VFS{regCppVFS},
			KV: map[string]string{
				"p":  "PY",
				"pc": "yellow",
			},
			Tags: []string{},
			TargetProperties: map[string]string{
				"module_dir": instance.Path,
			},
			Platform: string(instance.Platform.Target),
			Requirements: map[string]interface{}{
				"cpu":     float64(1),
				"network": "restricted",
				"ram":     float64(32),
			},
			DepRefs: []NodeRef{},
		}

		if py3Suffix {
			pyNode.TargetProperties["module_tag"] = "py3"
		}

		pyRef := ctx.emit.Emit(pyNode)

		// CC node compiling the generated `.reg3.cpp`. IsGenerated=true so
		// composeCCPaths reads the input from $(B)/<modPath>/<reg>.
		// IncludeInputs is the gen_py3_reg.py script (the reference graph's
		// reg3 CC node lists [<.reg3.cpp>, <gen_py3_reg.py>] as its only
		// inputs — no transitive header closure is scanned because the
		// generated source contains only registration stubs).
		ccIn := in
		ccIn.IsGenerated = true
		ccIn.Generator = pyRef
		ccIn.HasGenerator = true
		ccIn.Py3Suffix = py3Suffix
		ccIn.IncludeInputs = []VFS{genPy3RegScriptVFS}
		// PR-M3-final-surgical (fix 4): mirror upstream ordering — the
		// PyInit_/init_module_ defines added by `onpy_register` AFTER
		// `_PY3_REGISTER`'s `SRCS(GLOBAL …)` only attach to user-declared
		// sources; the synthetic reg3.cpp keeps the pre-call CFLAGS
		// snapshot. Strip the two define families from this CC's bundle.
		if len(in.CFlags) > 0 {
			filtered := make([]string, 0, len(in.CFlags))
			for _, f := range in.CFlags {
				if strings.HasPrefix(f, "-DPyInit_") || strings.HasPrefix(f, "-Dinit_module_") {
					continue
				}
				filtered = append(filtered, f)
			}
			ccIn.CFlags = filtered
		}

		ccRef, ccOut := EmitCC(instance, regCpp, ccIn, ctx.emit)

		refs = append(refs, ccRef)
		outputs = append(outputs, ccOut)
		// memberInputs feeds the .global.a aggregator. The CC's own input
		// list is [<reg3.cpp>, gen_py3_reg.py]; gen_py3_reg.py contributes
		// the archive-input added by the reference graph (the reg3.cpp
		// itself is BUILD_ROOT-rooted and PR-35y R7 strips those from the
		// AR aggregator).
		memberInputs = append(memberInputs, genPy3RegScriptVFS)
	}

	return refs, outputs, memberInputs
}

// emitEnumSrcs emits one EN node per GENERATE_ENUM_SERIALIZATION(*)
// declaration in d.enumSrcs. PR-M3-D.
//
// Algorithm:
//  1. Walk tools/enum_parser/enum_parser as a host tool to get its
//     LD NodeRef. Falls back to the canonical binary path when the
//     walk fails with a ParseError.
//  2. For each GenerateEnumSerializationStmt, scan the header's
//     transitive include closure (same scanner as CC nodes).
//  3. Collect cross-EN deps: any previously emitted EN output path
//     that appears in the header's include closure contributes its
//     NodeRef and path to the dep lists.
//  4. Call EmitEN, then record the output paths in ctx.enOutputs.
//
// EN nodes are always emitted on the TARGET platform (instance.Platform.Target),
// matching the reference graph (all 21 EN nodes in sg2.json are on
// default-linux-aarch64 even though enum_parser is a host x86_64 tool).
//
// When `consumerInputs` is non-nil, additionally emit one downstream CC
// per EN's `_serialized.cpp` output, returning per-CC `(refs, outputs,
// memberInputs)` for the caller to fold into the surrounding AR member
// accumulators. This is PR-M3-codegen-cc-enqueue: the EN-emitted
// `_serialized.cpp` is an implicit module source whose compiled `.o`
// archives alongside the declared SRCS (reference shape: every EN
// consumer's regular `.a` archives the EN-downstream `.o` after its
// regular `.cpp.o` members). `consumerInputs` must carry the consuming
// module's full CC compile bag (CFlags / CXXFlags / ADDINCL / etc.) so
// the downstream CC node matches the byte-shape of a hand-written SRCS
// entry in the same module. When nil, only EN nodes are emitted (the
// header-only branch path; no module compiles those `_serialized.cpp`
// in current M3 closure).
func emitEnumSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerAddInclGlobal []string, consumerInputs *ModuleCCInputs) (ccRefs []NodeRef, ccOutputs []VFS, memberInputsList [][]VFS) {
	if len(d.enumSrcs) == 0 {
		return nil, nil, nil
	}

	const enumParserPath = "tools/enum_parser/enum_parser"

	var (
		enumParserLD  NodeRef
		enumParserBin = enumParserBinaryPath
	)

	// Walk enum_parser as a HOST tool (x86_64).
	enumHostInst := NewToolInstance(ctx.host, enumParserPath, instance.Language)
	enumHostInst.Flags = inferFlagsFromPath(enumParserPath, true)

	if exc := Try(func() {
		result := genModule(ctx, enumHostInst)
		enumParserLD = result.LDRef
		enumParserBin = result.LDPath
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
		// ParseError: leave zero LD ref; enumParserBin stays at canonical fallback.
	}

	// Synthesize a ModuleCCInputs for the include scanner using the
	// module's own ADDINCL declarations plus the peer-global ADDINCL
	// set so that headers from transitive peer libraries (e.g. abseil,
	// protobuf) resolve correctly. Mirrors the ModuleCCInputs built for
	// CC nodes in the same module (PR-M3-F-3).
	scanIn := ModuleCCInputs{
		// PR-M3-F-6: same dedup as the main CC composer site.
		AddIncl:           mergeDedup(d.addIncl, nil),
		PeerAddInclGlobal: peerAddInclGlobal,
		SourceRoot:        ctx.sourceRoot,
	}

	for _, stmt := range d.enumSrcs {
		headerRel := stmt.Header
		withHeader := stmt.Variant == "with_header"

		// Scan the header's transitive include closure using the
		// target scanner. EN nodes always compile on the target axis;
		// the include search path mirrors a target CC node's search
		// path for this module.
		closure := walkClosure(ctx, instance, resolveSourceVFS(ctx, instance, headerRel, scanIn.SrcDir), scanIn)

		// Cross-EN deps: when a previously emitted EN node produced a
		// _serialized.h file (--header variant), and the current header's
		// include closure contains a file that EXPLICITLY #includes that
		// _serialized.h (under $(B)), the current EN node must
		// dep on that prior EN node.
		//
		// The include scanner cannot resolve $(B)/_serialized.h
		// paths (generated files absent at scan time). The correct signal
		// is a literal `#include <..._serialized.h>` in any source file
		// that IS in the scanner closure. Scan each closure file on disk
		// for _serialized.h include patterns and match them against known
		// EN outputs.
		var depENRefs []NodeRef
		var depENOutputs []VFS

		if len(ctx.enOutputs) > 0 {
			// Build a map from bare rel-path suffix → buildRootPath for
			// all known _serialized.h EN outputs. Key is the path a
			// source header would write in an #include angle-bracket
			// form, e.g. "devtools/ymake/diag/stats_enums.h_serialized.h".
			serializedHByRel := make(map[string]VFS, len(ctx.enOutputs))
			for buildRootPath := range ctx.enOutputs {
				if !buildRootPath.IsBuild() || !strings.HasSuffix(buildRootPath.Rel, "_serialized.h") {
					continue
				}
				serializedHByRel[buildRootPath.Rel] = buildRootPath
			}

			depSeen := map[NodeRef]struct{}{}

			if len(serializedHByRel) > 0 {
				// PR-AUDIT-3 D07: consult the scanner's parsed-directive
				// cache rather than re-opening every closure entry with
				// os.Open / bufio.NewScanner. The scanner already parsed
				// each header while building `closure`; IncludeDirectiveTargets
				// returns the cached target strings (the bare-rel form a
				// source header writes between `<...>` / `"..."`) with no
				// FS re-read. The match against serializedHByRel is
				// identical to the previous ad-hoc bracket extraction.
				enScanner := ctx.scannerTarget
				for _, srcAbsPath := range closure {
					targets := enScanner.IncludeDirectiveTargets(srcAbsPath)
					for _, includePath := range targets {
						if !strings.HasSuffix(includePath, "_serialized.h") {
							continue
						}
						buildRootPath, ok := serializedHByRel[includePath]
						if !ok {
							continue
						}
						ref := ctx.enOutputs[buildRootPath]
						if _, dup := depSeen[ref]; dup {
							continue
						}
						depSeen[ref] = struct{}{}
						depENRefs = append(depENRefs, ref)
						depENOutputs = append(depENOutputs, buildRootPath)
						// Also include the corresponding _serialized.cpp path.
						cppPath := Build(strings.TrimSuffix(buildRootPath.Rel, "_serialized.h") + "_serialized.cpp")
						if cppRef, ok2 := ctx.enOutputs[cppPath]; ok2 && cppRef == ref {
							depENOutputs = append(depENOutputs, cppPath)
						}
					}
				}
			}
		}

		// PR-M3-F-7-B: register EN outputs in the target scanner's CodegenRegistry
		// with populated EmitsIncludes. EN nodes always emit on the target axis.
		// Per enum_parser/main.cpp::WriteHeader:
		//   _serialized.h  includes util/generic/serialized_enum.h + the input header.
		//   _serialized.cpp includes the enum_serialization_runtime headers + util helpers.
		//
		// PR-AUDIT-6: registered BEFORE EmitEN so the EN node itself can walk its
		// _serialized.cpp via the registry to augment its `inputs` closure (REF's
		// EN node `inputs` includes the .cpp's transitive include set; this walk
		// is what surfaces dispatch_methods.h / ordered_pairs.h / enum_runtime.h
		// in the EN node's inputs).
		serializedCPPPath := Build(instance.Path + "/" + headerRel + "_serialized.cpp")
		var serializedHPath VFS
		if withHeader {
			serializedHPath = Build(instance.Path + "/" + headerRel + "_serialized.h")
		}
		if ctx.scannerTarget.codegen != nil {
			headerSrc := Source(instance.Path + "/" + headerRel)
			cppIncludes := []VFS{
				headerSrc,
				Source("tools/enum_parser/enum_parser/stdlib_deps.h"),
				Source("tools/enum_parser/enum_serialization_runtime/dispatch_methods.h"),
				Source("tools/enum_parser/enum_serialization_runtime/enum_runtime.h"),
				Source("tools/enum_parser/enum_serialization_runtime/ordered_pairs.h"),
				Source("util/generic/map.h"),
				Source("util/generic/serialized_enum.h"),
				Source("util/generic/singleton.h"),
				Source("util/generic/string.h"),
				Source("util/generic/typetraits.h"),
				Source("util/generic/vector.h"),
				Source("util/stream/output.h"),
				Source("util/string/cast.h"),
			}
			SortVFS(cppIncludes)
			ctx.scannerTarget.codegen.Register(&GeneratedFileInfo{
				ProducerKvP:   "EN",
				OutputPath:    serializedCPPPath,
				EmitsIncludes: cppIncludes,
			})
			if withHeader {
				// PR-M3-enum-parser-registry: include the sibling _serialized.cpp
				// so CC consumers that #include the _serialized.h transitively pull
				// the .cpp into their inputs and (via its EmitsIncludes) the
				// enum_serialization_runtime header set (dispatch_methods.h /
				// enum_runtime.h / ordered_pairs.h / stdlib_deps.h). REF bundles
				// the EN producer's .h and .cpp outputs together in every
				// downstream CC's inputs; mirroring that bundling through the
				// registry is the smallest mechanism that reproduces it.
				hIncludes := []VFS{
					headerSrc,
					serializedCPPPath,
					Source("util/generic/serialized_enum.h"),
				}
				SortVFS(hIncludes)
				ctx.scannerTarget.codegen.Register(&GeneratedFileInfo{
					ProducerKvP:   "EN",
					OutputPath:    serializedHPath,
					EmitsIncludes: hIncludes,
				})
			}
		}

		// PR-AUDIT-6: walk each cross-EN dep's _serialized.cpp to fold its
		// transitive closure into THIS EN node's `inputs`. REF's EN node walks
		// through cross-EN deps (e.g. dep_types depends on stats_enums via a
		// `#include "stats_enums.h_serialized.h"` in some closure file; the
		// cross-EN dep's `_serialized.cpp` carries the enum_runtime.h transitive
		// closure that reaches dispatch_methods.h / ordered_pairs.h).
		//
		// EN nodes without cross-EN deps (e.g. stats_enums itself, a leaf EN)
		// don't get this augmentation — matching REF's tight 2-input shape for
		// leaf EN nodes.
		//
		// Excluding headerSrc (EmitEN appends it separately) and depENOutputs
		// (likewise) prevents multiset duplicates. Also filter the source-header
		// `closure` against depENOutputs — the closure may include a
		// $(B)/_serialized.h entry that depENOutputs also names (the
		// scanner resolves both through the codegen registry / cross-EN dep
		// detection), and the duplicate fails L2 multiset equality.
		enClosureExcl := map[VFS]struct{}{
			Source(instance.Path + "/" + headerRel): {},
		}
		for _, p := range depENOutputs {
			enClosureExcl[p] = struct{}{}
		}
		filteredClosure := make([]VFS, 0, len(closure))
		for _, p := range closure {
			if _, drop := enClosureExcl[p]; drop {
				continue
			}
			filteredClosure = append(filteredClosure, p)
		}
		var crossCppClosure []VFS
		for _, depOut := range depENOutputs {
			if !strings.HasSuffix(depOut.Rel, "_serialized.cpp") {
				continue
			}
			sub := walkClosure(ctx, instance, depOut, scanIn)
			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}
				crossCppClosure = append(crossCppClosure, p)
			}
		}
		// PR-M3-G-3: walk OUR OWN _serialized.cpp output through the codegen
		// registry to fold its transitive include closure (util/generic/*,
		// libcxx/*, musl/* etc. reached via cppIncludes' EmitsIncludes) into
		// THIS EN node's `inputs`. REF's EN node inputs equal the consuming
		// CC node's inputs (both walk the same .h_serialized.cpp) for the
		// plain GENERATE_ENUM_SERIALIZATION variant. The WITH_HEADER variant
		// produces a `.h_serialized.h` that other ENs cross-consume; REF
		// keeps those EN nodes' inputs tight (source-header closure only,
		// no output-side augmentation), because the consumers absorb the
		// full closure on their side. The two WITH_HEADER usages in the
		// M3 closure (`diag/stats_enums.h`, `diag/trace_type_enums.h`) are
		// both emitted with source-header-only inputs in sg2.json.
		//
		// The $(B)/<output> path lookup goes through the codegen
		// registry (registered moments earlier) and follows EmitsIncludes;
		// subsequent children resolve via parseIncludes on the real
		// $(S) headers.
		var ownOutputClosure []VFS
		if !withHeader && ctx.scannerTarget.codegen != nil {
			sub := walkClosure(ctx, instance, serializedCPPPath, scanIn)
			for _, p := range sub {
				if _, drop := enClosureExcl[p]; drop {
					continue
				}
				ownOutputClosure = append(ownOutputClosure, p)
			}
		}
		enClosure := mergeDedupVFS(filteredClosure, crossCppClosure)
		enClosure = mergeDedupVFS(enClosure, ownOutputClosure)
		sort.Slice(enClosure, func(i, j int) bool { return enClosure[i].String() < enClosure[j].String() })

		// PR-M3-L0-codegen-deps-EV-PB: when this EN node's transitive closure
		// pulls in a PB/EV producer's $(B) output (e.g. an EN whose
		// header includes a header that #includes msg.ev.pb.h), the resulting
		// EN node must dep on that PB/EV producer — matching sg2.json shape
		// where `module_resolver.h_serialized.cpp` deps on the msg.ev/trace.ev
		// EV nodes + events_extension PB. Filter out the cross-EN dep refs
		// already in depENRefs so they aren't duplicated.
		augmentedDepENRefs := depENRefs
		if extra := resolveCodegenDepRefs(ctx, instance, enClosure, depENRefs...); len(extra) > 0 {
			augmentedDepENRefs = append(append([]NodeRef(nil), depENRefs...), extra...)
		}

		enRef, enOutPaths := EmitEN(
			instance,
			Source(instance.Path+"/"+headerRel),
			withHeader,
			enumParserLD,
			enumParserBin,
			augmentedDepENRefs,
			depENOutputs,
			enClosure,
			ctx.emit,
		)

		// Record outputs so later EN nodes can dep on them.
		for _, p := range enOutPaths {
			ctx.enOutputs[p] = enRef
		}

		// PR-M3-codegen-cc-enqueue: emit the downstream CC compiling
		// the EN-produced `_serialized.cpp` as an implicit module
		// source. The CC inherits the consuming module's full compile
		// bag (consumerInputs); composeCCPaths' IsGenerated branch
		// roots the output under $(B)/<instance.Path>/
		// <headerRel>_serialized.cpp{,.o} with `_/` infix when headerRel
		// contains a `/`. depPrefix is the cross-EN dep set the
		// reference graph places ahead of the consumer's own
		// `_serialized.cpp` in the CC's inputs[] (sg2.json
		// devtools/ymake/export_json_debug.h_serialized.cpp.o:
		// inputs[0..1] = [stats_enums.h_serialized.cpp,
		// stats_enums.h_serialized.h], inputs[2] = the consuming
		// .h_serialized.cpp).
		if consumerInputs != nil {
			cppRel := headerRel + "_serialized.cpp"
			// DepRefs: own EN + cross-EN dep refs. Reference shape
			// (sg2.json export_json_debug.h_serialized.cpp.o):
			// deps = [stats_enums-EN-uid, export_json_debug-EN-uid].
			allDepRefs := make([]NodeRef, 0, 1+len(depENRefs))
			allDepRefs = append(allDepRefs, enRef)
			allDepRefs = append(allDepRefs, depENRefs...)
			ccRef, ccOut, ccIns := emitCodegenDownstreamCC(ctx, instance, cppRel, depENOutputs, allDepRefs, *consumerInputs)
			ccRefs = append(ccRefs, ccRef)
			ccOutputs = append(ccOutputs, ccOut)
			memberInputsList = append(memberInputsList, ccIns)
		}
	}

	return ccRefs, ccOutputs, memberInputsList
}
// codegenRegForInstance returns the CodegenRegistry attached to the
// scanner picked by scannerFor. Returns nil when the scanner is nil
// (PR-AUDIT-4: dispatch lives in scannerFor, not duplicated here).
func codegenRegForInstance(ctx *genCtx, instance ModuleInstance) *CodegenRegistry {
	sc := ctx.scannerFor(instance)
	if sc == nil {
		return nil
	}
	return sc.codegen
}

// protoDirectImportIncludes parses the direct `import "..."` statements from a
// .proto or .ev source file and converts them to the generated header paths that
// protoc emits under $(B):
//
//   - import "x/y/z.proto"  → "$(B)/x/y/z.pb.h"
//   - import "x/y/z.ev"     → "$(B)/x/y/z.ev.pb.h"
//
// Only direct imports of the primary file are returned (no recursion). When the
// file cannot be read (missing source on disk at scan time) the function returns
// nil. Results are sorted lexicographically. Cited upstream pattern:
// proto_processor.cpp:43-56::TProtoIncludeProcessor::PrepareIncludes.
//
// PR-AUDIT-3: legitimate disk read — extracts structured `import` directives
// from a .proto/.ev source at registration time to populate its EmitsIncludes.
// NOT for closure walks. The architectural cleanup to route through a unified
// registry-resolved "structured-import extractor" lives in PR-AUDIT-3.D12 (still
// open) — keeping the (B) classification per audit doc §2 D12, §4 PR-AUDIT-3.
func protoDirectImportIncludes(sourceRoot, srcRel string) []VFS {
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []VFS
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "import ") {
			continue
		}
		start := strings.IndexByte(line, '"')
		end := strings.LastIndexByte(line, '"')
		if start < 0 || end <= start {
			continue
		}
		imp := line[start+1 : end]
		if strings.HasSuffix(imp, ".ev") {
			out = append(out, Build(strings.TrimSuffix(imp, ".ev")+".ev.pb.h"))
		} else if strings.HasSuffix(imp, ".proto") {
			base := strings.TrimSuffix(imp, ".proto")
			if imp == "google/protobuf/descriptor.proto" {
				// descriptor.pb.h is pre-committed, not a codegen output.
				// Upstream tree: contrib/libs/protobuf/src/google/protobuf/descriptor.pb.h
				out = append(out, Source(pbRuntimeBase+"google/protobuf/descriptor.pb.h"))
			} else {
				out = append(out, Build(base+".pb.h"))
			}
		}
	}
	SortVFS(out)
	return out
}

// cfIncludeDirectives parses `#include "..."` directives from a configure_file
// template (.cpp.in / .c.in). Only quoted includes are collected (angle-bracket
// forms are system headers resolved by the compiler search path, not by the
// registry). Returns Source-rooted VFSes, sorted lexicographically.
// Returns nil when the file cannot be read.
//
// PR-AUDIT-3: legitimate disk read — extracts structured `#include` directives
// from a .cpp.in/.c.in template at registration time to populate the CF output's
// EmitsIncludes. NOT for closure walks. The architectural cleanup to route
// through a unified registry-resolved "structured-import extractor" lives in
// PR-AUDIT-3.D12 / .D16 (still open); kept per audit doc §2 D12/D16.
func cfIncludeDirectives(diskPath string) []VFS {
	data, err := os.ReadFile(diskPath)
	if err != nil {
		return nil
	}
	var out []VFS
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#include ") {
			continue
		}
		start := strings.IndexByte(t, '"')
		if start < 0 {
			continue
		}
		end := strings.IndexByte(t[start+1:], '"')
		if end < 0 {
			continue
		}
		inc := t[start+1 : start+1+end]
		if inc != "" {
			out = append(out, Source(inc))
		}
	}
	SortVFS(out)
	return out
}

