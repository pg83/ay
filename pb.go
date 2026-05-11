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
	pbProtocModule       = "contrib/tools/protoc"
	pbCppStyleguideModule = "contrib/tools/protoc/plugins/cpp_styleguide"
)

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

// protoImportsDescriptor reports whether the .proto (or .ev) source file at
// `<sourceRoot>/<srcRel>` contains an import of "google/protobuf/descriptor.proto".
// Returns false when the file cannot be read (missing source → no descriptor dep).
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
func emitProtoSrcs(ctx *genCtx, instance ModuleInstance, d *moduleData) {
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

	// Emit PB nodes.
	for _, src := range protoSrcs {
		EmitPB(instance, src, cppStyleguideLDRef, protocLDRef,
			cppStyleguideBinary, protocBinary,
			"cpp_proto", ctx.sourceRoot, ctx.emit)

		// F-7-B: register the .pb.h output with its EmitsIncludes. The .pb.h
		// includes the .pb.h of every proto imported by this source.
		protoRelPath := instance.Path + "/" + src
		protoBase := strings.TrimSuffix(protoRelPath, ".proto")
		pbH := "$(BUILD_ROOT)/" + protoBase + ".pb.h"
		if reg := codegenRegForInstance(ctx, instance); reg != nil {
			reg.Register(&GeneratedFileInfo{
				ProducerKvP:   "PB",
				OutputPath:    pbH,
				EmitsIncludes: protoDirectImportIncludes(ctx.sourceRoot, protoRelPath),
			})
		}
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
			EmitEV(instance, src, cppStyleguideLDRef, protocLDRef, event2cppLDRef,
				cppStyleguideBinary, protocBinary, event2cppBinary,
				"cpp_proto", ctx.sourceRoot, ctx.emit)

			// F-7-B: register the .ev.pb.h output with EmitsIncludes derived from
			// the .ev source's direct imports.
			evRelPath := instance.Path + "/" + src
			evH := "$(BUILD_ROOT)/" + evRelPath + ".pb.h"
			if reg := codegenRegForInstance(ctx, instance); reg != nil {
				reg.Register(&GeneratedFileInfo{
					ProducerKvP:   "EV",
					OutputPath:    evH,
					EmitsIncludes: protoDirectImportIncludes(ctx.sourceRoot, evRelPath),
				})
			}
		}
	}
}
