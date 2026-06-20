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
	pbWrapperPath     = pbWrapperVFS.string()
	pbPyWrapperPath   = pbPyWrapperVFS.string()
	pbDescriptorProto = pbDescriptorVFS.string()
)

var protobufRuntimeHeaders = []VFS{
	source(pbRuntimeBase + "google/protobuf/arena.h"),
	source(pbRuntimeBase + "google/protobuf/arenastring.h"),
	source(pbRuntimeBase + "google/protobuf/extension_set.h"),
	source(pbRuntimeBase + "google/protobuf/generated_message_reflection.h"),
	source(pbRuntimeBase + "google/protobuf/generated_message_util.h"),
	source(pbRuntimeBase + "google/protobuf/io/coded_stream.h"),
	source(pbRuntimeBase + "google/protobuf/message.h"),
	source(pbRuntimeBase + "google/protobuf/metadata_lite.h"),
	source(pbRuntimeBase + "google/protobuf/port_def.inc"),
	source(pbRuntimeBase + "google/protobuf/port_undef.inc"),
	source(pbRuntimeBase + "google/protobuf/repeated_field.h"),
	source(pbRuntimeBase + "google/protobuf/unknown_field_set.h"),
}

var pbDescriptorImporterHeaders = []VFS{
	source(pbRuntimeBase + "google/protobuf/generated_message_bases.h"),
	source(pbRuntimeBase + "google/protobuf/map_entry.h"),
	source(pbRuntimeBase + "google/protobuf/map_entry_lite.h"),
	source(pbRuntimeBase + "google/protobuf/map_field.h"),
	source(pbRuntimeBase + "google/protobuf/map_field_inl.h"),
	source(pbRuntimeBase + "google/protobuf/map_field_lite.h"),
	source(pbRuntimeBase + "google/protobuf/reflection_ops.h"),
}

const (
	pbRuntimeBase = "contrib/libs/protobuf/src/"

	abslTstringBase = "contrib/restricted/abseil-cpp-tstring/"
)

type ResolvedCPPProtoPlugin struct {
	Spec   CppProtoPlugin
	LDRef  NodeRef
	Binary VFS
}

func emitPB(
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
	moduleTag STR,
	liteHeaders bool,
	extraPlugins []ResolvedCPPProtoPlugin,
	transitiveProtoImports []VFS,
	extraDepRefs []NodeRef,
	producerSourceInputs []VFS,
	blocks *PbArgBlocks,
	emit Emitter,
) NodeRef {
	na := emit.nodeArenas()

	moduleDir := instance.Path.rel()

	protoBase := strings.TrimSuffix(protoRelPath, ".proto")

	pbH := build(protoBase + ".pb.h")
	pbCC := build(protoBase + ".pb.cc")
	pbDepsH := build(protoBase + ".deps.pb.h")
	grpcPbCC := build(protoBase + ".grpc.pb.cc")
	grpcPbH := build(protoBase + ".grpc.pb.h")
	srcVFS := source(protoRelPath)

	if protoSrcOverride != 0 {
		srcVFS = protoSrcOverride
	}

	outputs := assembleProtoCmdOutputs(protoBase, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH, extraPlugins, liteHeaders, grpc)

	outsChunk := make([]STR, 0, len(outputs))

	for _, output := range outputs {
		outsChunk = append(outsChunk, (output).str())
	}

	cmdArgs := na.chunkList(blocks.head, outsChunk, blocks.mid, na.strList(internStr(protoRelPath)))

	if len(blocks.tail) > 0 {
		cmdArgs = append(cmdArgs, blocks.tail)
	}

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

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
	inputs = append(inputs, srcVFS)

	targetProps := TargetProperties{ModuleDir: moduleDir}

	if moduleTag != 0 {
		targetProps.ModuleTag = moduleTag
	}

	foreignDepRefs := depRefs(cppStyleguideLDRef, grpcCppLDRef, protocLDRef)

	for _, plugin := range extraPlugins {
		foreignDepRefs = append(foreignDepRefs, depRefs(plugin.LDRef)...)
	}

	// Producer refs for build-generated proto sources (e.g. RUN_ANTLR -lang
	// protobuf): without these the producer JV is unreachable from the LD
	// root closure and gets DFS-pruned at finalize.
	deps := append([]NodeRef(nil), extraDepRefs...)

	// A build-generated .proto (protoSrcOverride set) lives under $(B); protoc
	// runs from $(B) so its relative `-I=./` and the proto path resolve to the
	// generated tree. Source .protos run from $(S).
	protocCwd := "$(S)"

	if protoSrcOverride != 0 {
		protocCwd = "$(B)"
	}

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: cmdArgs,
			Cwd: internStr(protocCwd),
			Env: env}),
		Env: env,
		// transitiveProtoImports and producerSourceInputs (the producer's
		// transitive $(S) leaf sources behind a build-generated .proto — RUN_ANTLR
		// grammar / template / jar / scripts — matching upstream's flat source
		// closure) are shared caller slices: referenced as chunks, never copied.
		Inputs:           na.inputList(inputs, transitiveProtoImports, producerSourceInputs),
		Outputs:          outputs,
		KV:               KV{P: pkPB, PC: pcYellow},
		TargetProperties: targetProps,
		Requirements:     Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:          deps,
		ForeignDepRefs:   foreignDepRefs,
		Resources:        usesPython3,
	}

	return emit.emit(node)
}

// assembleProtoCmdOutputs returns the proto command's output list in
// $CPP_PROTO_OUTS order: the main .pb.h floated to the front, then — in ya.make
// statement order — any plugin declared before lite headers were turned on, the
// cpp_out group (.pb.cc + the lite-header .deps.pb.h), grpc, and the remaining
// plugins. The buildable (.cc/.cpp) subset of this list, in this order, is also
// the per-proto archive member order: ymake queues a command's outputs as module
// sources in this order (module_builder.cpp QueueCommandOutputs). Single source
// of truth shared by emitPB (the command) and emitProtoPB (the codegen compiles).
func assembleProtoCmdOutputs(protoBase string, pbH, pbCC, pbDepsH, grpcPbCC, grpcPbH VFS, extraPlugins []ResolvedCPPProtoPlugin, liteHeaders, grpc bool) []VFS {
	outputs := []VFS{pbH}

	for _, plugin := range extraPlugins {
		if !pluginOutputsPrecedeCppGroup(plugin, liteHeaders) {
			continue
		}

		for _, suffix := range plugin.Spec.OutputSuffixes {
			outputs = append(outputs, build(protoBase+suffix))
		}
	}

	outputs = append(outputs, pbCC)

	if liteHeaders {
		outputs = append(outputs, pbDepsH)
	}

	if grpc {
		outputs = append(outputs, grpcPbCC, grpcPbH)
	}

	for _, plugin := range extraPlugins {
		if pluginOutputsPrecedeCppGroup(plugin, liteHeaders) {
			continue
		}

		for _, suffix := range plugin.Spec.OutputSuffixes {
			outputs = append(outputs, build(protoBase+suffix))
		}
	}

	return outputs
}

// pluginOutputsPrecedeCppGroup reports whether a proto plugin's generated
// outputs are accumulated into CPP_PROTO_OUTS ahead of the cpp_out group
// (.pb.cc + the lite-header .deps.pb.h). That happens only when the plugin was
// declared before lite headers were turned on: the .deps.pb.h append is driven
// by the later SET(PROTOC_TRANSITIVE_HEADERS "no"), so a plugin declared first
// lands ahead of it. With transitive headers (no .deps.pb.h) plugins always
// follow the cpp_out group.
func pluginOutputsPrecedeCppGroup(plugin ResolvedCPPProtoPlugin, liteHeaders bool) bool {
	return liteHeaders && plugin.Spec.DeclaredBeforeLiteHeaders
}

func containsVFS(xs []VFS, want VFS) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}

	return false
}

func protoCPPOutRoot(d *ModuleData) string {
	if d.protoNamespace == nil {
		return ""
	}

	root := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(d.protoNamespace.string())), "/")

	if root == "." {
		return ""
	}

	return root
}

type ProtoSrcsResult struct {
	ARRef                NodeRef
	ARPath               *VFS
	GlobalRef            *NodeRef
	GlobalPath           *VFS
	WholeArchiveRefs     []NodeRef
	WholeArchivePaths    []VFS
	WholeArchiveCmdPaths []VFS
}

func protoSourceRelPath(fs FS, instance ModuleInstance, d *ModuleData, src string) string {
	return filepath.ToSlash(filepath.Clean(resolvePySrcRel(fs, d.srcDirs, instance.Path.rel(), src)))
}

func pyProtoAuxInputClosure(ctx *GenCtx, instance ModuleInstance, d *ModuleData, aux VFS, seed []VFS, ref NodeRef, peerAddIncl []VFS) []VFS {
	{
		rescompilerRef, _ := ctx.tool(argToolsRescompiler)

		emits := make([]IncludeDirective, 0, len(seed))

		for _, in := range seed {
			if in.isSource() {
				emits = append(emits, IncludeDirective{kind: includeQuoted, target: internStr(in.rel())})
			}
		}

		registerBoundGeneratedParsedOutput(ctx, instance, pkPR, aux, emits, ref, []NodeRef{rescompilerRef})
	}

	scanIn := ModuleCCInputs{
		TC:                d.tc,
		InclArgs:          ctx.inclArgs,
		Flags:             d.flags,
		AddIncl:           d.addIncl,
		PeerAddInclGlobal: peerAddIncl,
		FS:                ctx.fs,
		ScanCfg:           newScanContext(ctx.parsers, d.addIncl, peerAddIncl, includeScannerBasePaths(), instance.Path.rel()),
	}

	closure := walkClosure(ctx.scannerFor(instance), aux, scanIn.ScanCfg)

	if len(closure) == 0 {
		return nil
	}

	// The window is already deduplicated — no further dedup needed.
	return closure
}

func py3ccToolRefs(ctx *GenCtx, instance ModuleInstance) (NodeRef, NodeRef, VFS, VFS) {
	py3ccRef, py3ccBinary := ctx.tool(argToolsPy3cc)
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

// pbArgBlocks are the module-stable spans of a protoc (PB) command line —
// everything that does not depend on the individual .proto source:
//
//	head: [python3, cpp_proto_wrapper.py, --outputs]
//	mid:  [--, protoc, the -I set (own namespace, _PROTO__INCLUDE chain,
//	      namespace tail, runtime src), --cpp_out, extra protoc flags,
//	      the styleguide plugin]
//	tail: the grpc / extra plugin blocks (they follow the source token)
//
// Built once per module proto context (newPBModuleEmission) and referenced as
// chunks by every PB node of that context.
type PbArgBlocks struct {
	head []STR
	mid  []STR
	tail []STR
}

func composePBArgBlocks(tc ModuleToolchain, protocBinary, cppStyleguideBinary, grpcCppBinary VFS,
	grpc bool, cppOutRoot string, liteHeaders bool,
	extraProtocFlags []ARG, extraPlugins []ResolvedCPPProtoPlugin,
	protoInclude []VFS) *PbArgBlocks {
	head := []STR{
		tc.Python3,
		internStr(pbWrapperPath),
		argOutputs.str(),
	}

	includeRoot := ""

	if cppOutRoot != "" {
		includeRoot = cppOutRoot
	}

	cppOutArg := ":$(B)/" + cppOutRoot

	if liteHeaders {
		cppOutArg = "proto_h=true" + cppOutArg
	}

	mid := make([]STR, 0, 12+len(protoInclude)+len(extraProtocFlags))
	mid = append(mid,
		arg2.str(),
		(protocBinary).str(),
		internStr("-I=./"+includeRoot),
		internStr("-I=$(S)/"+includeRoot),
		argIB2.str(),
		argIS3.str(),
	)

	if cppOutRoot != "" {
		mid = append(mid, internStr("-I=$(S)/"+cppOutRoot))
	}

	// Upstream's _CPP_PROTO_CMDLINE_BASE (ymake.core.conf:614) renders
	// `${pre=-I=:_PROTO__INCLUDE}` VERBATIM — the FOR-proto GLOBAL ADDINCL set in
	// encounter order, with no dedup against the structural prefix. `protoInclude`
	// is the peers-only set (the module's own namespace rides the structural
	// `-I=$(S)/cppOutRoot` arm above). protobuf-src enters this set for modules
	// that transitively peer contrib/libs/protobuf (its ya.make declares
	// `ADDINCL GLOBAL FOR proto contrib/libs/protobuf/src`), so it shows up here
	// AS WELL AS via the trailing macro expansion (-I=$PROTOBUF_INCLUDE_PATH).
	// A peer that re-declares this module's own PROTO_NAMESPACE contributes the
	// same `FOR proto $(S)/<ns>` addincl, which lands in the set at the PEER's
	// encounter position (e.g. sg7 taxi_schemas_schemas_proto: -I=$(S)/taxi/...
	// after -I=$(S)/contrib/libs/protobuf/src) — rendering verbatim preserves it,
	// where a dedup against the structural own-namespace arm would not.
	for _, p := range protoInclude {
		mid = append(mid, internStr("-I="+p.string()))
	}

	mid = append(mid,
		argIB2.str(),
		argISContribLibsProtobufSrc.str(),
		internStr("--cpp_out="+cppOutArg),
	)
	mid = appendArgStr(mid, extraProtocFlags)
	mid = append(mid,
		internStr("--cpp_styleguide_out=:$(B)/"+cppOutRoot),
		internStr("--plugin=protoc-gen-cpp_styleguide="+cppStyleguideBinary.string()),
	)

	var tail []STR

	if grpc {
		tail = append(tail,
			internStr("--plugin=protoc-gen-grpc_cpp="+grpcCppBinary.string()),
			internStr("--grpc_cpp_out=$(B)/"+cppOutRoot),
		)
	}

	for _, plugin := range extraPlugins {
		tail = append(tail,
			internStr("--plugin=protoc-gen-"+plugin.Spec.Name+"="+plugin.Binary.string()),
			internStr("--"+plugin.Spec.Name+"_out=$(B)/"+cppOutRoot),
		)

		// Upstream's _PROTO_PLUGIN_ARGS_BASE expands `${pre=--${Name}_opt=:OutParm}`
		// over OutParm, whose commas are macro-argument separators: the
		// EXTRA_OUT_FLAG scalar splits on `,`, empty pieces drop, and each
		// surviving piece becomes its own `--${Name}_opt=<piece>`.
		for _, piece := range strings.Split(plugin.Spec.ExtraOutFlag, ",") {
			if piece == "" {
				continue
			}

			tail = append(tail, internStr("--"+plugin.Spec.Name+"_opt="+piece))
		}
	}

	return &PbArgBlocks{head: head, mid: mid, tail: tail}
}
