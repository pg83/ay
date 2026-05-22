package main

// ev.go — emitter for EV (event-log .ev → .ev.pb.cc/.ev.pb.h) nodes.
//
// Structurally identical to PB but appends three extra cmd_args for the
// event2cpp protoc plugin, and uses .ev.pb.cc/.ev.pb.h outputs (cc first).
//
// inputs = [cpp_styleguide, protoc, event2cpp, cpp_proto_wrapper.py,
//   $(S)/<module_dir>/<src>, ...transitive imports..., descriptor.proto?]
// Transitive imports come from scanning `import "..."` lines (then
// recursing). events_extension.proto + descriptor.proto are reached
// transitively. deps and foreign_deps["tool"] carry all three host LD refs.

const (
	// evEvent2cppModule is the ya.make path walked for the event2cpp host
	// LD. tools/event2cpp/ya.make uses INCLUDE() which we don't expand;
	// tools/event2cpp/bin/ya.make holds the PROGRAM declaration.
	// ldBinaryDir lifts the output dir back to tools/event2cpp.
	evEvent2cppModule = "tools/event2cpp/bin"
)

var (
	evEventlogIncludeVFS  = Source("library/cpp/eventlog")
	evEventlogIncludePath = evEventlogIncludeVFS.String()
)

// eventRuntimeHeaders is the SOURCE_ROOT header subset present in every
// .ev.pb.cc CC node (intersection across reference consumers). Registered
// as EmitsIncludes on the .ev.pb.h output so the scanner closure
// propagates them into all .ev.pb.h consumers. Sorted lexicographically.
var eventRuntimeHeaders = []VFS{
	Source("library/cpp/eventlog/event_field_output.h"),
	Source("library/cpp/eventlog/event_field_printer.h"),
	Source("library/cpp/eventlog/events_extension.h"),
	Source("util/charset/unicode_table.h"),
	Source("util/charset/unidata.h"),
	Source("util/digest/numeric.h"),
	Source("util/generic/array_size.h"),
	Source("util/generic/bitops.h"),
	Source("util/generic/buffer.h"),
	Source("util/generic/cast.h"),
	Source("util/generic/deque.h"),
	Source("util/generic/explicit_type.h"),
	Source("util/generic/flags.h"),
	Source("util/generic/fwd.h"),
	Source("util/generic/hide_ptr.h"),
	Source("util/generic/intrlist.h"),
	Source("util/generic/iterator.h"),
	Source("util/generic/map.h"),
	Source("util/generic/mapfindptr.h"),
	Source("util/generic/mem_copy.h"),
	Source("util/generic/noncopyable.h"),
	Source("util/generic/ptr.h"),
	Source("util/generic/refcount.h"),
	Source("util/generic/reserve.h"),
	Source("util/generic/singleton.h"),
	Source("util/generic/store_policy.h"),
	Source("util/generic/strbase.h"),
	Source("util/generic/strbuf.h"),
	Source("util/generic/string.h"),
	Source("util/generic/string_hash.h"),
	Source("util/generic/typelist.h"),
	Source("util/generic/typetraits.h"),
	Source("util/generic/utility.h"),
	Source("util/generic/va_args.h"),
	Source("util/generic/yexception.h"),
	Source("util/generic/ylimits.h"),
	Source("util/memory/alloc.h"),
	Source("util/memory/tempbuf.h"),
	Source("util/str_stl.h"),
	Source("util/stream/fwd.h"),
	Source("util/stream/input.h"),
	Source("util/stream/labeled.h"),
	Source("util/stream/output.h"),
	Source("util/stream/str.h"),
	Source("util/stream/tempbuf.h"),
	Source("util/stream/zerocopy.h"),
	Source("util/stream/zerocopy_output.h"),
	Source("util/string/hex.h"),
	Source("util/string/subst.h"),
	Source("util/system/align.h"),
	Source("util/system/atexit.h"),
	Source("util/system/backtrace.h"),
	Source("util/system/compat.h"),
	Source("util/system/compiler.h"),
	Source("util/system/defaults.h"),
	Source("util/system/error.h"),
	Source("util/system/guard.h"),
	Source("util/system/mutex.h"),
	Source("util/system/platform.h"),
	Source("util/system/src_location.h"),
	Source("util/system/src_root.h"),
	Source("util/system/thread.i"),
	Source("util/system/type_name.h"),
	Source("util/system/types.h"),
	Source("util/system/unaligned_mem.h"),
	Source("util/system/win_undef.h"),
	Source("util/system/winint.h"),
	Source("util/system/yassert.h"),
}

// evExtraProtobufHeaders is the .ev.pb.h-specific protobuf header subset
// appearing only in CC consumers of event-generated headers (from
// event2cpp's reflection codegen + plugin scaffolding). Sorted lex.
var evExtraProtobufHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/io/printer.h"),
	Source(pbRuntimeBase + "google/protobuf/io/zero_copy_sink.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/hash.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/stringpiece.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/strutil.h"),
}

// evAbseilCleanupHeaders is the abseil RAII-cleanup pair propagated through
// every .ev.pb.h to its CC consumers (verified in sg2.json).
var evAbseilCleanupHeaders = []VFS{
	Source("contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h"),
	Source("contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h"),
}

// evWitnessExtras returns the .ev.pb.h-specific witness inputs propagated
// through every .ev.pb.h to its CC consumers: cpp_proto_wrapper.py,
// descriptor.proto, the .ev source, the companion .ev.pb.cc,
// pbDescriptorImporterHeaders, evExtraProtobufHeaders, and
// evAbseilCleanupHeaders. Every .ev imports descriptor.proto
// transitively, so no source-scan is needed.
func evWitnessExtras(evRelPath string, evPbCC VFS) []includeDirective {
	out := make([]includeDirective, 0,
		3+len(pbDescriptorImporterHeaders)+len(evExtraProtobufHeaders)+len(evAbseilCleanupHeaders))
	out = append(out, includeDirective{kind: includeQuoted, target: pbWrapperVFS.Rel})
	out = append(out, includeDirective{kind: includeQuoted, target: pbDescriptorVFS.Rel})
	out = append(out, includeDirective{kind: includeQuoted, target: evRelPath})
	out = append(out, includeDirective{kind: includeQuoted, target: evPbCC.Rel})
	for _, v := range pbDescriptorImporterHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: v.Rel})
	}
	for _, v := range evExtraProtobufHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: v.Rel})
	}
	for _, v := range evAbseilCleanupHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: v.Rel})
	}

	return out
}

// EmitEV emits an EV node for `evRelPath` (a SOURCE_ROOT-relative .ev path).
func EmitEV(
	instance ModuleInstance,
	evRelPath string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	event2cppLDRef NodeRef,
	cppStyleguideBinary VFS,
	protocBinary VFS,
	event2cppBinary VFS,
	moduleTag *string,
	transitiveImports []VFS,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path

	// EV outputs: .ev.pb.cc first, then .ev.pb.h (reference order).
	evCC := Build(evRelPath + ".pb.cc")
	evH := Build(evRelPath + ".pb.h")
	srcVFS := Source(evRelPath)

	cmdArgs := []string{
		instance.Platform.Tools.Python3,
		pbWrapperPath,
		"--outputs",
		evCC.String(),
		evH.String(),
		"--",
		protocBinary.String(),
		"-I=./",
		"-I=$(S)/",
		"-I=$(B)",
		"-I=$(S)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"-I=$(B)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"--cpp_out=:$(B)/",
		"--cpp_styleguide_out=:$(B)/",
		"--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary.String(),
		evRelPath,
		"--plugin=protoc-gen-event2cpp=" + event2cppBinary.String(),
		"--event2cpp_out=$(B)",
		"-I=" + evEventlogIncludePath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// Build inputs: tool binaries + wrapper + source + transitive imports.
	inputs := []VFS{
		cppStyleguideBinary,
		protocBinary,
		event2cppBinary,
		pbWrapperVFS,
		srcVFS,
	}

	inputs = append(inputs, transitiveImports...)

	targetProps := map[string]string{
		"module_dir": moduleDir,
	}

	if moduleTag != nil {
		targetProps["module_tag"] = *moduleTag
	}

	// deps and foreign_deps carry all three tool refs.
	var depRefs []NodeRef
	var foreignDepRefs map[string][]NodeRef

	{
		var toolRefs []NodeRef
		if cppStyleguideLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}
		if protocLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, protocLDRef)
		}
		if event2cppLDRef != (NodeRef{}) {
			toolRefs = append(toolRefs, event2cppLDRef)
		}
		if len(toolRefs) > 0 {
			depRefs = append([]NodeRef(nil), toolRefs...)
			foreignDepRefs = map[string][]NodeRef{"tool": toolRefs}
		}
	}

	node := &Node{
		Cmds: []Cmd{
			{
				CmdArgs: cmdArgs,
				Cwd:     "$(S)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  inputs,
		Outputs: []VFS{evCC, evH},
		KV: map[string]interface{}{
			"p":  "EV",
			"pc": "yellow",
		},
		Tags:             instance.Platform.Tags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		Requirements: map[string]interface{}{
			"cpu":     float64(1),
			"network": "restricted",
			"ram":     float64(32),
		},
		DepRefs:        depRefs,
		ForeignDepRefs: foreignDepRefs,
	}

	return emit.Emit(bindNodePlatform(node, instance.Platform))
}
