package main

import (
	"crypto/md5"
	"encoding/base32"
	enchex "encoding/hex"
	"path/filepath"
	"sort"
	"strings"
)

var (
	pbWrapperPath     = pbWrapperVFS.String()
	pbPyWrapperPath   = pbPyWrapperVFS.String()
	pbDescriptorProto = pbDescriptorVFS.String()
)

var protobufRuntimeHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/arena.h"),
	Source(pbRuntimeBase + "google/protobuf/arenastring.h"),
	Source(pbRuntimeBase + "google/protobuf/extension_set.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_message_reflection.h"),
	Source(pbRuntimeBase + "google/protobuf/generated_message_util.h"),
	Source(pbRuntimeBase + "google/protobuf/io/coded_stream.h"),
	Source(pbRuntimeBase + "google/protobuf/message.h"),
	Source(pbRuntimeBase + "google/protobuf/metadata_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/port_def.inc"),
	Source(pbRuntimeBase + "google/protobuf/port_undef.inc"),
	Source(pbRuntimeBase + "google/protobuf/repeated_field.h"),
	Source(pbRuntimeBase + "google/protobuf/unknown_field_set.h"),
}

var pbDescriptorImporterHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/generated_message_bases.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry.h"),
	Source(pbRuntimeBase + "google/protobuf/map_entry_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_inl.h"),
	Source(pbRuntimeBase + "google/protobuf/map_field_lite.h"),
	Source(pbRuntimeBase + "google/protobuf/reflection_ops.h"),
}

const (
	pbRuntimeBase = "contrib/libs/protobuf/src/"

	abslTstringBase = "contrib/restricted/abseil-cpp-tstring/"
)

type resolvedCPPProtoPlugin struct {
	Spec   cppProtoPlugin
	LDRef  NodeRef
	Binary VFS
}

func EmitPB(
	instance ModuleInstance,
	protoRelPath string,
	protoSrcOverride VFS,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	grpcCppLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	grpcCppBinary VFS,
	grpc bool,
	moduleTag *string,
	cppOutRoot string,
	duplicateOutputRootInclude bool,
	liteHeaders bool,
	extraProtocFlags []ARG,
	extraPlugins []resolvedCPPProtoPlugin,
	transitiveProtoImports []VFS,
	hasDescriptor bool,
	peerProtoAddIncl []VFS,
	extraDepRefs []NodeRef,
	producerSourceInputs []VFS,
	tc moduleToolchain,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path

	protoBase := strings.TrimSuffix(protoRelPath, ".proto")

	pbH := Build(protoBase + ".pb.h")
	pbCC := Build(protoBase + ".pb.cc")
	pbDepsH := Build(protoBase + ".deps.pb.h")
	grpcPbCC := Build(protoBase + ".grpc.pb.cc")
	grpcPbH := Build(protoBase + ".grpc.pb.h")
	srcVFS := Source(protoRelPath)

	if protoSrcOverride != 0 {
		srcVFS = protoSrcOverride
	}

	outputs := []VFS{pbH, pbCC}

	if liteHeaders {
		outputs = append(outputs, pbDepsH)
	}

	if grpc {
		outputs = append(outputs, grpcPbCC, grpcPbH)
	}

	for _, plugin := range extraPlugins {
		for _, suffix := range plugin.Spec.OutputSuffixes {
			outputs = append(outputs, Build(protoBase+suffix))
		}
	}

	cmdArgs := []STR{
		tc.Python3,
		internStr(pbWrapperPath),
		argOutputs.str(),
	}

	for _, output := range outputs {
		cmdArgs = append(cmdArgs, (output).str())
	}

	includeRoot := ""

	if cppOutRoot != "" {
		includeRoot = cppOutRoot
	}

	cppOutArg := ":$(B)/" + cppOutRoot

	if liteHeaders {
		cppOutArg = "proto_h=true" + cppOutArg
	}

	cmdArgs = append(cmdArgs,
		arg2.str(),
		(protocBinary).str(),
		internStr("-I=./"+includeRoot),
		internStr("-I=$(S)/"+includeRoot),
		argIB2.str(),
		argIS3.str(),
	)

	if cppOutRoot != "" {
		cmdArgs = append(cmdArgs, internStr("-I=$(S)/"+cppOutRoot))

		if duplicateOutputRootInclude {
			cmdArgs = append(cmdArgs, internStr("-I=$(S)/"+cppOutRoot))
		}
	}

	// Upstream's _CPP_PROTO_CMDLINE_BASE (ymake.core.conf:612) emits
	// `${pre=-I=:_PROTO__INCLUDE} -I=$ARCADIA_BUILD_ROOT
	// -I=$PROTOBUF_INCLUDE_PATH` — peers first, then $(B), then protobuf-src.
	// _PROTO__INCLUDE already contains protobuf-src for LIBRARY modules that
	// transitively peer contrib/libs/protobuf (its ya.make declares
	// `ADDINCL GLOBAL FOR proto contrib/libs/protobuf/src`), so the protobuf
	// -I shows up via the peer loop AS WELL AS via the trailing macro
	// expansion. PROTO_LIBRARY filters peers to CPP_PROTO-tagged modules
	// (proto.conf:921), so contrib/libs/protobuf's FOR proto addincl does
	// NOT enter its peer chain — only PROTO_LIBRARY-internal protos
	// (which need it via `ADDINCL GLOBAL FOR proto contrib/libs/protobuf/src`
	// from their own peers).
	for _, p := range peerProtoAddIncl {
		cmdArgs = append(cmdArgs, internStr("-I="+p.String()))
	}

	if moduleTag == nil && strings.HasPrefix(protoRelPath, "yt/") {
		cmdArgs = append(cmdArgs, argISYt.str())
	}

	cmdArgs = append(cmdArgs,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internStr("--cpp_out="+cppOutArg),
	)
	cmdArgs = appendArgStr(cmdArgs, extraProtocFlags)
	cmdArgs = append(cmdArgs,
		internStr("--cpp_styleguide_out=:$(B)/"+cppOutRoot),
		internStr("--plugin=protoc-gen-cpp_styleguide="+cppStyleguideBinary.String()),
		internStr(protoRelPath),
	)

	if grpc {
		cmdArgs = append(cmdArgs,
			internStr("--plugin=protoc-gen-grpc_cpp="+grpcCppBinary.String()),
			internStr("--grpc_cpp_out=$(B)/"+cppOutRoot),
		)
	}

	for _, plugin := range extraPlugins {
		cmdArgs = append(cmdArgs,
			internStr("--plugin=protoc-gen-"+plugin.Spec.Name+"="+plugin.Binary.String()),
			internStr("--"+plugin.Spec.Name+"_out=$(B)/"+cppOutRoot),
		)

		if plugin.Spec.ExtraOutFlag != "" {
			cmdArgs = append(cmdArgs, internStr("--"+plugin.Spec.Name+"_opt=:"+plugin.Spec.ExtraOutFlag))
		}
	}

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputs := []VFS{
		cppStyleguideBinary,
	}

	if grpc {
		inputs = append(inputs, grpcCppBinary)
	}

	inputs = append(inputs, protocBinary)

	for _, plugin := range extraPlugins {
		inputs = append(inputs, plugin.Binary)
	}

	inputs = append(inputs, pbWrapperVFS)

	if hasDescriptor {
		inputs = append(inputs, pbDescriptorVFS)
	}

	inputs = append(inputs, srcVFS)
	inputs = append(inputs, transitiveProtoImports...)
	// When srcVFS is build-generated, carry the producer's transitive $(S) leaf
	// sources (e.g. the RUN_ANTLR grammar / template / jar / scripts behind a
	// generated .proto) so the PB node's input set matches upstream's flat
	// source closure.
	inputs = append(inputs, producerSourceInputs...)

	targetProps := TargetProperties{ModuleDir: moduleDir}

	if moduleTag != nil {
		targetProps.ModuleTag = *moduleTag
	}

	var depRefs []NodeRef
	var foreignDepRefs []NodeRef

	if cppStyleguideLDRef != (NodeRef(0)) || protocLDRef != (NodeRef(0)) || grpcCppLDRef != (NodeRef(0)) || len(extraPlugins) > 0 {
		var toolRefs []NodeRef

		if cppStyleguideLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}

		if grpcCppLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, grpcCppLDRef)
		}

		if protocLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, protocLDRef)
		}

		for _, plugin := range extraPlugins {
			if plugin.LDRef == (NodeRef(0)) {
				continue
			}

			toolRefs = append(toolRefs, plugin.LDRef)
		}

		depRefs = append([]NodeRef(nil), toolRefs...)
		foreignDepRefs = toolRefs
	}

	// Producer refs for build-generated proto sources (e.g. RUN_ANTLR -lang
	// protobuf): without these the producer JV is unreachable from the LD
	// root closure and gets DFS-pruned at finalize.
	depRefs = append(depRefs, extraDepRefs...)

	// A build-generated .proto (protoSrcOverride set) lives under $(B); protoc
	// runs from $(B) so its relative `-I=./` and the proto path resolve to the
	// generated tree. Source .protos run from $(S).
	protocCwd := "$(S)"

	if protoSrcOverride != 0 {
		protocCwd = "$(B)"
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     internStr(protocCwd),
				Env:     env,
			},
		},
		Env:              env,
		Inputs:           inputs,
		Outputs:          outputs,
		KV:               KV{P: pkPB, PC: pcYellow},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform))
}

func containsVFS(xs []VFS, want VFS) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}

	return false
}

func protoCPPModulePath(instance ModuleInstance, d *moduleData) string {
	if d != nil && d.protoNamespace != nil {
		if d.protoNamespaceGlobal {
			return instance.Path
		}

		base := filepath.ToSlash(filepath.Clean(filepath.Dir(*d.protoNamespace)))

		if base != "." && base != "" {
			return base
		}
	}

	return instance.Path
}

func protoCPPOutRoot(d *moduleData) string {
	if d == nil || d.protoNamespace == nil {
		return ""
	}

	root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(*d.protoNamespace)), "/")

	if root == "." {
		return ""
	}

	return root
}

type protoSrcsResult struct {
	ARRef                NodeRef
	ARPath               *VFS
	GlobalRef            *NodeRef
	GlobalPath           *VFS
	WholeArchiveRefs     []NodeRef
	WholeArchivePaths    []VFS
	WholeArchiveCmdPaths []VFS
}

func protoSourceRelPath(fs FS, instance ModuleInstance, d *moduleData, src string) string {
	moduleRel := filepath.ToSlash(filepath.Clean(instance.Path + "/" + src))

	if fs.IsFile(dirKey(instance.Path), src) {
		return moduleRel
	}

	baseDir := instance.Path

	if d.srcDir != nil {
		cleaned := filepath.Clean(*d.srcDir)

		if cleaned != "." {
			baseDir = cleaned
		}
	}

	return filepath.ToSlash(filepath.Clean(baseDir + "/" + src))
}

func pyProtoAuxInputClosure(ctx *genCtx, instance ModuleInstance, d *moduleData, aux VFS, seed []VFS, peerAddIncl []VFS) []VFS {
	reg := codegenRegForInstance(ctx, instance)

	if reg != nil {
		rescompilerRef, _ := ctx.tool(argToolsRescompilerBin)

		emits := make([]includeDirective, 0, len(seed))

		for _, in := range seed {
			if in.IsSource() {
				emits = append(emits, includeDirective{kind: includeQuoted, target: internStr(in.Rel())})
			}
		}

		registerGeneratedParsedOutput(ctx, instance, "PR", aux, emits, []NodeRef{rescompilerRef})
	}

	scanIn := ModuleCCInputs{
		TC:                d.tc,
		InclArgs:          ctx.inclArgs,
		Flags:             d.flags,
		AddIncl:           d.addIncl,
		PeerAddInclGlobal: peerAddIncl,
		SourceRoot:        ctx.sourceRoot,
		FS:                ctx.fs,
	}

	closure := walkClosure(ctx, instance, aux, scanIn)

	if len(closure) == 0 {
		return nil
	}

	// walkClosure already returns a deduplicated window — no further dedup needed.
	return closure
}

func py3ccToolRefs(ctx *genCtx, instance ModuleInstance) (NodeRef, NodeRef, VFS, VFS) {
	py3ccRef, py3ccRaw := ctx.tool(argToolsPy3ccBin)
	py3ccBinary := canonicalizePy3ccBinary(py3ccRaw)
	py3ccSlowRef, py3ccSlowBin := ctx.tool(argToolsPy3ccSlow)
	return py3ccRef, py3ccSlowRef, py3ccBinary, py3ccSlowBin
}

func protoPySuffix(modulePath string) string {
	return protoPathID("$S/" + modulePath)[:4]
}

func protoPathID(path string) string {
	sum := md5.Sum([]byte(path))
	encoded := base32.StdEncoding.EncodeToString(sum[:])
	encoded = strings.ToLower(encoded)
	return strings.TrimRight(encoded, "=")
}

func protoResourceHash(items []string, modulePath, moduleTag string) string {
	list := append([]string(nil), items...)
	list = append(list, modulePath)
	sort.Strings(list)

	sum := md5.Sum([]byte(strings.Join(list, ",") + moduleTag))
	return strings.ToLower(enchex.EncodeToString(sum[:]))[:26]
}
