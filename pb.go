package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// pb.go — emitter for PB (Protocol Buffers compile) nodes.
//
// EmitPB emits one PB node per .proto source in a PROTO_LIBRARY.
// Each node invokes cpp_proto_wrapper.py (a Python wrapper) which
// calls protoc with the cpp_styleguide plugin. The wrapper and both
// tool binaries come from contrib/tools/protoc (host programs).
//
// Reference shape (18 cmd_args, verified against sg2.json):
//
//	/ix/realm/pg/bin/python3
//	$(SOURCE_ROOT)/build/scripts/cpp_proto_wrapper.py
//	--outputs <.pb.h> <.pb.cc>
//	--
//	$(BUILD_ROOT)/contrib/tools/protoc/protoc
//	-I=./ -I=$(SOURCE_ROOT)/ -I=$(BUILD_ROOT) -I=$(SOURCE_ROOT)
//	-I=$(SOURCE_ROOT)/contrib/libs/protobuf/src
//	-I=$(BUILD_ROOT) -I=$(SOURCE_ROOT)/contrib/libs/protobuf/src
//	--cpp_out=:$(BUILD_ROOT)/
//	--cpp_styleguide_out=:$(BUILD_ROOT)/
//	--plugin=protoc-gen-cpp_styleguide=<cpp_styleguide_binary>
//	<module_dir/proto_file>
//
// inputs = [cpp_styleguide_binary, protoc_binary, cpp_proto_wrapper.py,
//           $(SOURCE_ROOT)/<module_dir>/<src>,
//           optionally $(SOURCE_ROOT)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto]
//
// descriptor.proto is included in inputs when the .proto source imports
// "google/protobuf/descriptor.proto" (detected by scanning the source
// file for that import string).
//
// foreign_deps / deps both carry [cpp_styleguide_LD_ref, protoc_LD_ref]
// (two tool refs; the order matches the reference graph's uid list).
//
// tags: ["tool"] when platform == x86_64 (host build), [] otherwise.
// target_properties: module_dir (always) + module_tag:"cpp_proto" (always).

const (
	pbPython3Path       = "/ix/realm/pg/bin/python3"
	pbWrapperPath       = "$(SOURCE_ROOT)/build/scripts/cpp_proto_wrapper.py"
	pbProtocBinaryPath  = "$(BUILD_ROOT)/contrib/tools/protoc/protoc"
	pbCppStyleguidePath = "$(BUILD_ROOT)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide"
	pbDescriptorProto   = "$(SOURCE_ROOT)/contrib/libs/protobuf/src/google/protobuf/descriptor.proto"

	// Tool module paths for host-walk recursion.
	pbProtocModule        = "contrib/tools/protoc"
	pbCppStyleguideModule = "contrib/tools/protoc/plugins/cpp_styleguide"

	// pbRuntimeBase is the $(SOURCE_ROOT)-rooted prefix for all protobuf
	// runtime headers (under contrib/libs/protobuf/src/).
	pbRuntimeBase = "$(SOURCE_ROOT)/contrib/libs/protobuf/src/"
)

// protobufRuntimeHeaders is the set of headers that every protoc-generated
// .pb.h directly #includes (verified by reading any.pb.h, duration.pb.h,
// timestamp.pb.h, etc.). These are registered as EmitsIncludes on the .pb.h
// output so the scanner closure propagates them into all CC nodes that
// include the .pb.h. Scanner recursion then finds their transitive includes.
// Sorted lexicographically. VFS-rooted $(SOURCE_ROOT)/... paths.
var protobufRuntimeHeaders = []string{
	pbRuntimeBase + "google/protobuf/arena.h",
	pbRuntimeBase + "google/protobuf/arenastring.h",
	pbRuntimeBase + "google/protobuf/extension_set.h",
	pbRuntimeBase + "google/protobuf/generated_message_reflection.h",
	pbRuntimeBase + "google/protobuf/generated_message_util.h",
	pbRuntimeBase + "google/protobuf/io/coded_stream.h",
	pbRuntimeBase + "google/protobuf/message.h",
	pbRuntimeBase + "google/protobuf/metadata_lite.h",
	pbRuntimeBase + "google/protobuf/port_def.inc",
	pbRuntimeBase + "google/protobuf/port_undef.inc",
	pbRuntimeBase + "google/protobuf/repeated_field.h",
	pbRuntimeBase + "google/protobuf/unknown_field_set.h",
}

// pbDescriptorImporterHeaders are the protobuf runtime headers that appear in
// CC consumers of any .pb.h whose source proto imports
// "google/protobuf/descriptor.proto". These pull in the
// map/reflection_ops cluster that protoc emits in the reflection metadata for
// extension-bearing protos (verified by intersecting the inputs of every
// descriptor.proto-importing .pb.h's CC consumer in sg2.json — see
// docs/drafts/20260512-0200-residue-pre-100pct.md §2 lever #1).
// Sorted lexicographically.
var pbDescriptorImporterHeaders = []string{
	pbRuntimeBase + "google/protobuf/generated_message_bases.h",
	pbRuntimeBase + "google/protobuf/map_entry.h",
	pbRuntimeBase + "google/protobuf/map_entry_lite.h",
	pbRuntimeBase + "google/protobuf/map_field.h",
	pbRuntimeBase + "google/protobuf/map_field_inl.h",
	pbRuntimeBase + "google/protobuf/map_field_lite.h",
	pbRuntimeBase + "google/protobuf/reflection_ops.h",
}

// EmitPB emits a PB node for `srcRel` (a .proto file relative to `instance.Path`).
// `cppStyleguideLDRef` and `protocLDRef` are the host LD NodeRefs for the two
// tool programs (zeroed when the host walk failed). `cppStyleguideBinary` and
// `protocBinary` are the $(BUILD_ROOT)-rooted paths for the tool binaries.
// `moduleTag` is "cpp_proto" for PROTO_LIBRARY modules (may be empty for future use).
// `sourceRoot` is the absolute path to the source tree root (for descriptor-import scanning).
//
// Returns the emitted NodeRef.
func EmitPB(
	instance ModuleInstance,
	srcRel string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	cppStyleguideBinary string,
	protocBinary string,
	moduleTag string,
	sourceRoot string,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path
	protoRelPath := moduleDir + "/" + srcRel
	// Output paths strip the .proto suffix: foo.proto → foo.pb.h / foo.pb.cc.
	protoBase := strings.TrimSuffix(protoRelPath, ".proto")

	pbH := "$(BUILD_ROOT)/" + protoBase + ".pb.h"
	pbCC := "$(BUILD_ROOT)/" + protoBase + ".pb.cc"
	srcAbs := "$(SOURCE_ROOT)/" + protoRelPath

	cmdArgs := []string{
		pbPython3Path,
		pbWrapperPath,
		"--outputs",
		pbH,
		pbCC,
		"--",
		protocBinary,
		"-I=./",
		"-I=$(SOURCE_ROOT)/",
		"-I=$(BUILD_ROOT)",
		"-I=$(SOURCE_ROOT)",
		"-I=$(SOURCE_ROOT)/contrib/libs/protobuf/src",
		"-I=$(BUILD_ROOT)",
		"-I=$(SOURCE_ROOT)/contrib/libs/protobuf/src",
		"--cpp_out=:$(BUILD_ROOT)/",
		"--cpp_styleguide_out=:$(BUILD_ROOT)/",
		"--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary,
		protoRelPath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	// inputs: [cpp_styleguide, protoc, wrapper, source, optionally descriptor.proto]
	inputs := []string{
		cppStyleguideBinary,
		protocBinary,
		pbWrapperPath,
		srcAbs,
	}

	// If the source file imports "google/protobuf/descriptor.proto", add descriptor.proto.
	if protoImportsDescriptor(sourceRoot, moduleDir+"/"+srcRel) {
		inputs = append(inputs, pbDescriptorProto)
	}

	// tags: ["tool"] on host (x86_64), [] on target.
	tags := []string{}
	if targetIsX8664(instance) {
		tags = []string{"tool"}
	}

	targetProps := map[string]string{
		"module_dir": moduleDir,
	}

	if moduleTag != "" {
		targetProps["module_tag"] = moduleTag
	}

	// deps and foreign_deps both carry the two tool refs.
	var depRefs []NodeRef
	var foreignDepRefs map[string][]NodeRef

	if cppStyleguideLDRef != (NodeRef{}) || protocLDRef != (NodeRef{}) {
		var toolRefs []NodeRef
		if cppStyleguideLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}
		if protocLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, protocLDRef)
		}
		depRefs = append([]NodeRef(nil), toolRefs...)
		foreignDepRefs = map[string][]NodeRef{"tool": toolRefs}
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(SOURCE_ROOT)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  inputs,
		Outputs: []string{pbH, pbCC},
		KV: map[string]string{
			"p":  "PB",
			"pc": "yellow",
		},
		Tags:             tags,
		TargetProperties: targetProps,
		Platform: string(instance.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: foreignDepRefs,
	}

	return emit.Emit(node)
}

// pbDescriptorImporterExtras returns the witness inputs propagated through a
// protoc-generated .pb.h whose source proto imports
// "google/protobuf/descriptor.proto". The list is the union of:
//   - pbDescriptorImporterHeaders (7 protobuf reflection-cluster headers),
//   - pbWrapperPath (cpp_proto_wrapper.py — the script that drives protoc),
//   - pbDescriptorProto (the descriptor.proto source itself),
//   - the proto source file (its $(SOURCE_ROOT)-rooted path).
//
// Returns nil when the proto does not import descriptor.proto.
//
// Verified by intersecting CC-consumer inputs across all
// descriptor.proto-importing .pb.h's in
// /home/pg/monorepo/yatool_orig/sg2.json (see
// docs/drafts/20260512-0200-residue-pre-100pct.md §2 lever #1).
func pbDescriptorImporterExtras(sourceRoot, protoRelPath string) []string {
	if !protoImportsDescriptor(sourceRoot, protoRelPath) {
		return nil
	}

	out := make([]string, 0, len(pbDescriptorImporterHeaders)+3)
	out = append(out, pbWrapperPath)
	out = append(out, pbDescriptorProto)
	out = append(out, "$(SOURCE_ROOT)/"+protoRelPath)
	out = append(out, pbDescriptorImporterHeaders...)

	return out
}

// protoImportsDescriptor reports whether the .proto (or .ev) source file at
// `<sourceRoot>/<srcRel>` contains an import of "google/protobuf/descriptor.proto".
// Returns false when the file cannot be read (missing source → no descriptor dep).
//
// PR-AUDIT-3: legitimate disk read — extracts a single structured `import`
// predicate from a .proto/.ev source at PB-node-emission time. NOT for closure
// walks. The architectural cleanup to route this through a unified
// registry-resolved "structured-import extractor" lives in PR-AUDIT-3.D12
// (still open); kept per audit doc §2 D12.
func protoImportsDescriptor(sourceRoot, srcRel string) bool {
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)

	if err != nil {
		return false
	}

	defer f.Close()

	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, `"google/protobuf/descriptor.proto"`) {
			return true
		}
	}

	return false
}

// emitProtoSrcs emits PB/EV nodes for .proto and .ev entries in d.srcs
// when the module is a PROTO_LIBRARY. Called from the header-only
// branch of genModule after peer-walking, before returning the result.
// PB/EV emitters walk the host protoc + cpp_styleguide tool instances to
// get their LDRefs; the same cached instances are shared across all
// PROTO_LIBRARY modules via the genCtx memo.
//
// PR-M3-proto-library-ar: for true PROTO_LIBRARY modules (module name
// `PROTO_LIBRARY`), after emitting PB/EV nodes, this function ALSO
// emits the downstream CC for each generated .pb.cc / .ev.pb.cc and an
// AR archiving them into `lib<dotted-path>.a` with module_tag=cpp_proto.
// Mirrors the LIBRARY/EV branch in `gen.go::emitOneSource` (the .ev
// case at line 4315) for the per-source downstream-CC dispatch; mirrors
// the LIBRARY AR shape at line 3097 for the archive step.
// `peerContribs` carries the transitive per-axis peer-GLOBAL union the
// header-only walker computed (used to compose the per-CC ModuleCCInputs
// so flags reach the consumer CCs of the protoc-generated sources).
func emitProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData, peerContribs peerGlobalContribs) {
	// Collect .proto and .ev sources from d.srcs.
	var protoSrcs, evSrcs []string

	for _, src := range d.srcs {
		switch {
		case strings.HasSuffix(src, ".proto"):
			protoSrcs = append(protoSrcs, src)
		case strings.HasSuffix(src, ".ev"):
			evSrcs = append(evSrcs, src)
		}
	}

	if len(protoSrcs) == 0 && len(evSrcs) == 0 {
		return
	}

	// Walk host protoc and cpp_styleguide tool programs.
	cppStyleguideBinary := pbCppStyleguidePath
	protocBinary := pbProtocBinaryPath

	var cppStyleguideLDRef, protocLDRef NodeRef

	protocHostInst := instance.WithHost(ctx.cfg)
	protocHostInst.Path = pbProtocModule
	protocHostInst.Flags = inferFlagsFromPath(pbProtocModule, true)

	if exc := Try(func() {
		result := genModule(ctx, protocHostInst)
		protocLDRef = result.LDRef
		protocBinary = result.LDPath
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	cppStyleguideHostInst := instance.WithHost(ctx.cfg)
	cppStyleguideHostInst.Path = pbCppStyleguideModule
	cppStyleguideHostInst.Flags = inferFlagsFromPath(pbCppStyleguideModule, true)

	if exc := Try(func() {
		result := genModule(ctx, cppStyleguideHostInst)
		cppStyleguideLDRef = result.LDRef
		cppStyleguideBinary = result.LDPath
	}); exc != nil {
		// Swallow ParseError; use canonical fallback path.
		_ = exc
	}

	// PR-M3-proto-library-ar: collect per-codegen-source (genRef, .pb.cc path)
	// pairs so the AR step can fold them into ccRefs/ccOutputs/memberInputs
	// in declaration order. Mirrors the LIBRARY AR aggregation pattern
	// (gen.go:2761 addMemberInputs(ccIns) inside the per-source loop).
	type protoCodegenOutput struct {
		genRef NodeRef // PB or EV node ref (used as Generator dep for the downstream CC)
		pbCC   string  // generated .pb.cc / .ev.pb.cc absolute BUILD_ROOT path
		srcRel string  // module-relative source-with-codegen-suffix (".pb.cc" appended)
		primSrc string // primary source path ($(SOURCE_ROOT)/<module>/<src>) for AR memberInputs
	}

	var codegenOutputs []protoCodegenOutput

	// Emit PB nodes.
	for _, src := range protoSrcs {
		pbRef := EmitPB(instance, src, cppStyleguideLDRef, protocLDRef,
			cppStyleguideBinary, protocBinary,
			"cpp_proto", ctx.sourceRoot, ctx.emit)

		// F-7-B: register the .pb.h output with its EmitsIncludes. The .pb.h
		// includes the .pb.h of every proto imported by this source, plus the
		// constant protobuf runtime header set (F-7-D).
		protoRelPath := instance.Path + "/" + src
		protoBase := strings.TrimSuffix(protoRelPath, ".proto")
		pbH := "$(BUILD_ROOT)/" + protoBase + ".pb.h"
		pbCC := "$(BUILD_ROOT)/" + protoBase + ".pb.cc"
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			directImports := protoDirectImportIncludes(ctx.sourceRoot, protoRelPath)
			extras := pbDescriptorImporterExtras(ctx.sourceRoot, protoRelPath)
			emitsIncludes := make([]string, 0, len(directImports)+len(protobufRuntimeHeaders)+len(extras))
			emitsIncludes = append(emitsIncludes, directImports...)
			emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
			emitsIncludes = append(emitsIncludes, extras...)
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PB",
				OutputPath:    pbH,
				EmitsIncludes: emitsIncludes,
			})
			// PR-AUDIT-6 step 4: register the .pb.cc output too. protoc emits a
			// `#include "<base>.pb.h"` plus the protobuf runtime headers; the
			// .pb.h's own EmitsIncludes are already registered (just above), so a
			// single entry pointing at the .pb.h would suffice — we mirror the
			// .pb.h list for symmetry with the LIBRARY/EV path (gen.go:4338-4342).
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PB",
				OutputPath:    pbCC,
				EmitsIncludes: append([]string{pbH}, protobufRuntimeHeaders...),
			})
		}

		// PR-M3-proto-library-ar: stash the (PB ref, .pb.cc, src-with-suffix)
		// for the downstream-CC + AR step below.
		codegenOutputs = append(codegenOutputs, protoCodegenOutput{
			genRef:  pbRef,
			pbCC:    pbCC,
			srcRel:  strings.TrimSuffix(src, ".proto") + ".pb.cc",
			primSrc: "$(SOURCE_ROOT)/" + protoRelPath,
		})
	}

	// Emit EV nodes (PROTO_LIBRARY with .ev sources → module_tag:"cpp_proto").
	if len(evSrcs) > 0 {
		event2cppBinary := evEvent2cppBinaryPath
		var event2cppLDRef NodeRef

		event2cppHostInst := instance.WithHost(ctx.cfg)
		event2cppHostInst.Path = evEvent2cppModule
		event2cppHostInst.Flags = inferFlagsFromPath(evEvent2cppModule, true)

		if exc := Try(func() {
			result := genModule(ctx, event2cppHostInst)
			event2cppLDRef = result.LDRef
			event2cppBinary = result.LDPath
		}); exc != nil {
			_ = exc
		}

		for _, src := range evSrcs {
			evRef := EmitEV(instance, src, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				"cpp_proto", ctx.sourceRoot, ctx.emit)

			// F-7-B: register the .ev.pb.h output with EmitsIncludes derived from
			// the .ev source's direct imports, plus the protobuf runtime headers (F-7-D)
			// and the EV-specific runtime headers (util/* + eventlog).
			evRelPath := instance.Path + "/" + src
			evH := "$(BUILD_ROOT)/" + evRelPath + ".pb.h"
			evPbCC := "$(BUILD_ROOT)/" + evRelPath + ".pb.cc"
			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				directImports := protoDirectImportIncludes(ctx.sourceRoot, evRelPath)
				evExtras := evWitnessExtras(ctx.sourceRoot, evRelPath, evPbCC)
				emitsIncludes := make([]string, 0, len(directImports)+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders)+len(evExtras))
				emitsIncludes = append(emitsIncludes, directImports...)
				emitsIncludes = append(emitsIncludes, protobufRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, eventRuntimeHeaders...)
				emitsIncludes = append(emitsIncludes, evExtras...)
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evH,
					EmitsIncludes: emitsIncludes,
				})
				// PR-AUDIT-6 step 4: register the .ev.pb.cc output too. event2cpp
				// emits a `#include "<base>.ev.pb.h"` plus the protobuf + event
				// runtime headers; mirror the .pb.h list for symmetry with the
				// LIBRARY/EV path (gen.go:4338-4342).
				ccEmits := make([]string, 0, 1+len(protobufRuntimeHeaders)+len(eventRuntimeHeaders))
				ccEmits = append(ccEmits, evH)
				ccEmits = append(ccEmits, protobufRuntimeHeaders...)
				ccEmits = append(ccEmits, eventRuntimeHeaders...)
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evPbCC,
					EmitsIncludes: ccEmits,
				})
			}

			codegenOutputs = append(codegenOutputs, protoCodegenOutput{
				genRef:  evRef,
				pbCC:    evPbCC,
				srcRel:  src + ".pb.cc",
				primSrc: "$(SOURCE_ROOT)/" + evRelPath,
			})
		}
	}

	// PR-M3-proto-library-ar: for true PROTO_LIBRARY modules, emit the
	// downstream CC for each generated .pb.cc / .ev.pb.cc and the AR
	// archiving them. Skip for non-PROTO_LIBRARY callers — the LIBRARY
	// path's own .ev branch in emitOneSource already handles its own
	// downstream-CC + AR aggregation (gen.go:4315).
	if d.moduleStmt.Name != "PROTO_LIBRARY" || len(codegenOutputs) == 0 {
		return
	}

	// Compose ModuleCCInputs for the downstream CCs. Mirror the LIBRARY
	// path's moduleInputs construction (gen.go:2632) but pull the per-axis
	// peer-GLOBAL slices from the header-only walker's peerContribs.
	// LibcMusl-self modules zero their own GLOBAL CFLAGS (mirror of
	// gen.go:1925-1929 in the header-only branch).
	ownCFlagsGlobalSelf := d.cFlagsGlobal
	ownCXXFlagsGlobalSelf := d.cxxFlagsGlobal
	ownCOnlyFlagsGlobalSelf := d.cOnlyFlagsGlobal

	if instance.Flags.LibcMusl {
		ownCFlagsGlobalSelf = nil
		ownCXXFlagsGlobalSelf = nil
		ownCOnlyFlagsGlobalSelf = nil
	}

	dedupedAddIncl := mergeDedup(d.addIncl, nil)

	moduleInputs := ModuleCCInputs{
		AddIncl:              dedupedAddIncl,
		PeerAddInclGlobal:    peerContribs.addIncl,
		CFlags:               d.cFlags,
		CXXFlags:             d.cxxFlags,
		COnlyFlags:           d.cOnlyFlags,
		OwnCFlagsGlobal:      ownCFlagsGlobalSelf,
		OwnCXXFlagsGlobal:    ownCXXFlagsGlobalSelf,
		OwnCOnlyFlagsGlobal:  ownCOnlyFlagsGlobalSelf,
		PeerCFlagsGlobal:     peerContribs.cFlags,
		PeerCXXFlagsGlobal:   peerContribs.cxxFlags,
		PeerCOnlyFlagsGlobal: peerContribs.cOnlyFlags,
		AutoPeerCFlags:       defaultPeerCFlags(ctx, instance, d),
		SrcDir:               d.srcDir,
		SourceRoot:           ctx.sourceRoot,
		DefaultVars:          d.defaultVars,
		DefaultVarOrder:      d.defaultVarOrder,
		ModuleTag:            "cpp_proto",
	}

	// Per-source downstream-CC emission. Mirrors gen.go:4399-4411 (EV
	// LIBRARY branch) but for the PROTO_LIBRARY context.
	ccRefs := make([]NodeRef, 0, len(codegenOutputs))
	ccOutputs := make([]string, 0, len(codegenOutputs))
	memberInputs := make([]string, 0, 64)
	memberInputsSeen := make(map[string]struct{})

	addMemberInputs := func(paths []string) {
		for _, p := range paths {
			if _, dup := memberInputsSeen[p]; dup {
				continue
			}
			memberInputsSeen[p] = struct{}{}
			memberInputs = append(memberInputs, p)
		}
	}

	for _, co := range codegenOutputs {
		ccIn := moduleInputs
		ccIn.IsGenerated = true
		ccIn.Generator = co.genRef
		ccIn.HasGenerator = true
		ccIn.IncludeInputs = walkClosure(ctx, instance, co.pbCC, moduleInputs)

		ccRef, ccOut := EmitCC(instance, co.srcRel, ccIn, ctx.emit)
		ccRefs = append(ccRefs, ccRef)
		ccOutputs = append(ccOutputs, ccOut)

		// AR memberInputs: primary source first, then the CC's include closure.
		// Mirror of gen.go:4414-4415 (LIBRARY EV branch returning the .ev
		// source as the primary member input) + gen.go:2761 addMemberInputs.
		perCC := make([]string, 0, 1+len(ccIn.IncludeInputs))
		perCC = append(perCC, co.primSrc)
		perCC = append(perCC, ccIn.IncludeInputs...)
		addMemberInputs(perCC)
	}

	// AR emission. Mirrors gen.go:3097 EmitARNamed with module_tag=cpp_proto.
	arBaseName := ArchiveName(instance.Path)
	archivePath := "$(BUILD_ROOT)/" + instance.Path + "/" + arBaseName
	emitARNode(instance, archivePath, "cpp_proto", ccRefs, ccOutputs, nil, memberInputs, ctx.emit)
}
