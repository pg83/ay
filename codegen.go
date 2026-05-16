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

	// ENABLE(PYBUILD_NO_PYC) suppresses yapyc3 generation; modules like
	// contrib/tools/python3/lib2/py embed sources via RESOURCE/objcopy.
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
		// canonicalizePy3ccBinaryPath: $(B)/tools/py3cc/bin/py3cc →
		// $(B)/tools/py3cc/py3cc to match the reference yapyc3 cmd_args[0].
		// tools/py3cc/bin/ya.make declares SRCDIR(tools/py3cc) so the
		// upstream intent is a top-level binary.
		py3ccBinary = canonicalizePy3ccBinaryPath(result.LDPath)
	}); exc != nil {
		var pe *ParseError
		if !errors.As(exc.AsError(), &pe) {
			panic(exc)
		}
		// Leave zero ref; py3ccBinary stays at canonical fallback.
	}

	// Walk tools/py3cc/slow. tools/py3cc/slow/ya.make uses
	// IF(NOT PREBUILT) INCLUDE(bin/ya.make); our parser expands it
	// (PREBUILT=false). tools/py3cc/slow/bin declares PY3_PROGRAM_BIN
	// which isMultimoduleLibraryType routes to a header-only path, so
	// LDPath is empty. Only update py3ccSlowBin when non-empty.
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

	// Walk tools/rescompiler/bin, tools/rescompressor/bin, tools/archiver
	// as host tools — referenced by PY (objcopy) and AR (pyc.inc) nodes.
	// Walks are eager (memoized in ctx).
	const (
		rescompilerBinPath   = "tools/rescompiler/bin"
		rescompressorBinPath = "tools/rescompressor/bin"
		archiverPath         = "tools/archiver"
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

		// Output suffix: flat → .py.yapyc3; subdir →
		// .py.<pathid($S/unit)[:4]>.yapyc3.
		var outputPath VFS
		if strings.Contains(srcRel, "/") {
			outputPath = Build(instance.Path + "/" + srcRel + "." + pySrcYapycSuffix(instance.Path) + ".yapyc3")
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
				// PY23_LIBRARY's .yapyc3 nodes carry `module_tag=py3`
				// in REF (MODULE_TAG=PY3 from _ARCADIA_PYTHON3_ADDINCL
				// via the PY3 submodule). PY3_LIBRARY etc keep no tag
				// (upstream omits the redundant default).
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

		// Wire py3cc LD refs into both DepRefs and
		// ForeignDepRefs["tool"] to match REF. Skip zero refs
		// (host walk failed → no LD node to reference).
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

		// Register the .yapyc3 output in the codegen registry so the
		// downstream objcopy CC's input-driven resolveCodegenDepRefsExt
		// lookup threads the PY producer into its deps[].
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

// emitPyRegister emits PY+CC pair for each PY_REGISTER(arg) in
// d.pyRegister:
//   - PY:  python3 gen_py3_reg.py <arg> $(B)/<modPath>/<arg>.reg3.cpp
//   - CC:  compiles `.reg3.cpp` → `.reg3.cpp.o` (or `.reg3.cpp.py3.o`
//     when py3Suffix is set).
//
// Both refs flow into globalRefs/globalOutputs — _PY3_REGISTER emits
// `SRCS(GLOBAL ...)`, so the CC output lands in `.global.a`.
// Mirror of _PY3_REGISTER at build/ymake.core.conf:4086-4089.
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

		// CC node compiling `.reg3.cpp`. IsGenerated=true so
		// composeCCPaths reads from $(B)/<modPath>/<reg>. The
		// reference reg3 CC node lists only [.reg3.cpp, gen_py3_reg.py]
		// — no transitive header scan (generated stub).
		ccIn := in
		ccIn.IsGenerated = true
		ccIn.Generator = pyRef
		ccIn.HasGenerator = true
		ccIn.Py3Suffix = py3Suffix
		ccIn.IncludeInputs = []VFS{genPy3RegScriptVFS}
		// PyInit_/init_module_ defines added by `onpy_register` AFTER
		// `_PY3_REGISTER`'s `SRCS(GLOBAL …)` attach only to user-declared
		// sources; the synthetic reg3.cpp keeps the pre-call CFLAGS
		// snapshot. Strip the two families from this CC's bundle.
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

		ccRef, ccOut := EmitCC(instance, regCpp, ccIn, ctx.host, ctx.emit)

		refs = append(refs, ccRef)
		outputs = append(outputs, ccOut)
		// memberInputs feeds the .global.a aggregator. CC's own inputs
		// = [reg3.cpp, gen_py3_reg.py]; only gen_py3_reg.py contributes
		// (reg3.cpp is BUILD_ROOT-rooted and AR aggregator strips those).
		memberInputs = append(memberInputs, genPy3RegScriptVFS)
	}

	return refs, outputs, memberInputs
}

// emitEnumSrcs emits one EN node per GENERATE_ENUM_SERIALIZATION(*)
// in d.enumSrcs.
//
// Algorithm:
//  1. Walk tools/enum_parser/enum_parser as host tool → LD NodeRef
//     (ParseError → canonical binary path fallback).
//  2. For each stmt, scan the header's transitive include closure
//     (same scanner as CC nodes).
//  3. Cross-EN deps: any previously emitted EN output appearing in
//     the closure contributes its NodeRef and path.
//  4. Call EmitEN, record outputs in ctx.enOutputs.
//
// EN nodes are always emitted on the TARGET platform; enum_parser is
// host x86_64 but the EN outputs are target-axis.
//
// When `consumerInputs` is non-nil, also emit one downstream CC per
// `_serialized.cpp` output (the EN-emitted .cpp is an implicit module
// source archived alongside declared SRCS). consumerInputs must carry
// the consuming module's full CC compile bag. nil → EN nodes only.
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
	// module's own ADDINCL + peer-global ADDINCL set so headers from
	// transitive peer libraries (abseil, protobuf) resolve correctly.
	// Mirrors the ModuleCCInputs built for CC nodes in the same module.
	scanIn := ModuleCCInputs{
		AddIncl:           mergeDedup(d.addIncl, nil),
		PeerAddInclGlobal: peerAddInclGlobal,
		SourceRoot:        ctx.sourceRoot,
	}

	for _, stmt := range d.enumSrcs {
		headerRel := stmt.Header
		withHeader := stmt.Variant == "with_header"

		// Scan the header's transitive include closure with the target
		// scanner (EN nodes always compile on the target axis).
		closure := walkClosure(ctx, instance, resolveSourceVFS(ctx, instance, headerRel, scanIn.SrcDir), scanIn)

		// Cross-EN deps: when a previously emitted EN produced a
		// _serialized.h (--header variant) and the current header's
		// closure contains a file `#include`-ing that _serialized.h,
		// the current EN deps on the prior one. The scanner cannot
		// resolve $(B)/_serialized.h (absent at scan time); the signal
		// is a literal `#include` in any closure file matching a known
		// EN output.
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
				// Consult the scanner's parsed-directive cache rather
				// than re-opening each closure entry. The scanner
				// already parsed each header while building `closure`;
				// IncludeDirectiveTargets returns cached target strings.
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

		// Register EN outputs in the target scanner's CodegenRegistry
		// with populated EmitsIncludes (EN emits on target axis).
		// Per enum_parser/main.cpp::WriteHeader:
		//   _serialized.h  → util/generic/serialized_enum.h + input header.
		//   _serialized.cpp → enum_serialization_runtime headers + util.
		//
		// Registered BEFORE EmitEN so the EN node can walk its
		// _serialized.cpp via the registry to augment its `inputs`
		// closure (surfaces dispatch_methods.h / ordered_pairs.h /
		// enum_runtime.h in the EN node's inputs).
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
				// Include the sibling _serialized.cpp so CC consumers
				// that #include the _serialized.h transitively pull the
				// .cpp into their inputs and (via its EmitsIncludes) the
				// enum_serialization_runtime header set. REF bundles
				// the EN producer's .h and .cpp outputs together in
				// every downstream CC's inputs.
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

		// Walk each cross-EN dep's _serialized.cpp to fold its transitive
		// closure into THIS EN node's `inputs`. The cross-EN dep's .cpp
		// carries the enum_runtime.h closure (dispatch_methods.h,
		// ordered_pairs.h). Leaf EN nodes (no cross-EN deps) keep REF's
		// tight 2-input shape.
		//
		// Exclude headerSrc and depENOutputs (EmitEN appends them
		// separately) and filter the source-header closure against
		// depENOutputs to avoid multiset duplicates.
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
		// Walk OUR OWN _serialized.cpp output through the codegen
		// registry to fold its transitive include closure into THIS EN
		// node's `inputs`. REF's EN node inputs equal the consuming CC
		// node's inputs for the plain variant; WITH_HEADER variants
		// keep tight source-header-only inputs (consumers absorb the
		// full closure on their side).
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

		// When this EN's transitive closure pulls in a PB/EV producer's
		// $(B) output (e.g. EN whose header eventually #includes
		// msg.ev.pb.h), the EN node deps on that producer. Filter out
		// refs already in depENRefs.
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

		// Emit the downstream CC compiling the EN-produced
		// `_serialized.cpp` as an implicit module source. The CC
		// inherits consumerInputs (full compile bag). depPrefix is the
		// cross-EN dep set placed ahead of the consumer's own
		// `_serialized.cpp` in the CC's inputs[].
		if consumerInputs != nil {
			cppRel := headerRel + "_serialized.cpp"
			// DepRefs: own EN + cross-EN dep refs.
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
// scanner picked by scannerFor (nil-safe).
func codegenRegForInstance(ctx *genCtx, instance ModuleInstance) *CodegenRegistry {
	sc := ctx.scannerFor(instance)
	if sc == nil {
		return nil
	}
	return sc.codegen
}

// protoDirectImportIncludes parses direct `import "..."` statements
// from a .proto/.ev source and converts them to protoc's $(B) outputs:
//
//	import "x/y/z.proto" → "$(B)/x/y/z.pb.h"
//	import "x/y/z.ev"    → "$(B)/x/y/z.ev.pb.h"
//
// Direct imports only (no recursion). Returns nil on read failure;
// results sorted. Upstream pattern:
// proto_processor.cpp:43-56::TProtoIncludeProcessor::PrepareIncludes.
//
// Legitimate disk read: extracts structured `import` directives at
// registration time to populate EmitsIncludes. NOT for closure walks.
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

// cfIncludeDirectives parses `#include "..."` directives from a
// configure_file template (.cpp.in / .c.in). Quoted only (angle-bracket
// forms are system headers, resolved by the compiler search path).
// Returns Source-rooted VFSes sorted; nil on read failure.
//
// Legitimate disk read: extracts structured `#include` directives at
// registration time to populate EmitsIncludes. NOT for closure walks.
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
