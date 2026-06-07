package main

var (
	evEventlogIncludeVFS  = Intern("$(S)/library/cpp/eventlog")
	evEventlogIncludePath = evEventlogIncludeVFS.String()
	// Path constants hoisted by `ay refac consts`.
	anyCppOutB                  = internAny("--cpp_out=:$(B)/")
	anyCppStyleguideOutB        = internAny("--cpp_styleguide_out=:$(B)/")
	anyEvent2cppOutB            = internAny("--event2cpp_out=$(B)")
	anyI2                       = internAny("-I=./")
	anyIB2                      = internAny("-I=$(B)")
	anyIS2                      = internAny("-I=$(S)/")
	anyIS3                      = internAny("-I=$(S)")
	anyISContribLibsProtobufSrc = internAny("-I=$(S)/contrib/libs/protobuf/src")
	anyOutputs                  = internAny("--outputs")
)

var eventRuntimeHeaders = []VFS{
	Intern("$(S)/library/cpp/eventlog/event_field_output.h"),
	Intern("$(S)/library/cpp/eventlog/event_field_printer.h"),
	Intern("$(S)/library/cpp/eventlog/events_extension.h"),
	Intern("$(S)/util/charset/unicode_table.h"),
	Intern("$(S)/util/charset/unidata.h"),
	Intern("$(S)/util/digest/numeric.h"),
	Intern("$(S)/util/generic/array_size.h"),
	Intern("$(S)/util/generic/bitops.h"),
	Intern("$(S)/util/generic/buffer.h"),
	Intern("$(S)/util/generic/cast.h"),
	Intern("$(S)/util/generic/deque.h"),
	Intern("$(S)/util/generic/explicit_type.h"),
	Intern("$(S)/util/generic/flags.h"),
	Intern("$(S)/util/generic/fwd.h"),
	Intern("$(S)/util/generic/hide_ptr.h"),
	Intern("$(S)/util/generic/intrlist.h"),
	Intern("$(S)/util/generic/iterator.h"),
	Intern("$(S)/util/generic/map.h"),
	Intern("$(S)/util/generic/mapfindptr.h"),
	Intern("$(S)/util/generic/mem_copy.h"),
	Intern("$(S)/util/generic/noncopyable.h"),
	Intern("$(S)/util/generic/ptr.h"),
	Intern("$(S)/util/generic/refcount.h"),
	Intern("$(S)/util/generic/reserve.h"),
	Intern("$(S)/util/generic/singleton.h"),
	Intern("$(S)/util/generic/store_policy.h"),
	Intern("$(S)/util/generic/strbase.h"),
	Intern("$(S)/util/generic/strbuf.h"),
	Intern("$(S)/util/generic/string.h"),
	Intern("$(S)/util/generic/string_hash.h"),
	Intern("$(S)/util/generic/typelist.h"),
	Intern("$(S)/util/generic/typetraits.h"),
	Intern("$(S)/util/generic/utility.h"),
	Intern("$(S)/util/generic/va_args.h"),
	Intern("$(S)/util/generic/yexception.h"),
	Intern("$(S)/util/generic/ylimits.h"),
	Intern("$(S)/util/memory/alloc.h"),
	Intern("$(S)/util/memory/tempbuf.h"),
	Intern("$(S)/util/str_stl.h"),
	Intern("$(S)/util/stream/fwd.h"),
	Intern("$(S)/util/stream/input.h"),
	Intern("$(S)/util/stream/labeled.h"),
	Intern("$(S)/util/stream/output.h"),
	Intern("$(S)/util/stream/str.h"),
	Intern("$(S)/util/stream/tempbuf.h"),
	Intern("$(S)/util/stream/zerocopy.h"),
	Intern("$(S)/util/stream/zerocopy_output.h"),
	Intern("$(S)/util/string/hex.h"),
	Intern("$(S)/util/string/subst.h"),
	Intern("$(S)/util/system/align.h"),
	Intern("$(S)/util/system/atexit.h"),
	Intern("$(S)/util/system/backtrace.h"),
	Intern("$(S)/util/system/compat.h"),
	Intern("$(S)/util/system/compiler.h"),
	Intern("$(S)/util/system/defaults.h"),
	Intern("$(S)/util/system/error.h"),
	Intern("$(S)/util/system/guard.h"),
	Intern("$(S)/util/system/mutex.h"),
	Intern("$(S)/util/system/platform.h"),
	Intern("$(S)/util/system/src_location.h"),
	Intern("$(S)/util/system/src_root.h"),
	Intern("$(S)/util/system/thread.i"),
	Intern("$(S)/util/system/type_name.h"),
	Intern("$(S)/util/system/types.h"),
	Intern("$(S)/util/system/unaligned_mem.h"),
	Intern("$(S)/util/system/win_undef.h"),
	Intern("$(S)/util/system/winint.h"),
	Intern("$(S)/util/system/yassert.h"),
}

var evExtraProtobufHeaders = []VFS{
	Source(pbRuntimeBase + "google/protobuf/io/printer.h"),
	Source(pbRuntimeBase + "google/protobuf/io/zero_copy_sink.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/hash.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/stringpiece.h"),
	Source(pbRuntimeBase + "google/protobuf/stubs/strutil.h"),
}

var evAbseilCleanupHeaders = []VFS{
	Intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h"),
	Intern("$(S)/contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h"),
}

const (
	evEvent2cppModule = "tools/event2cpp/bin"
)

func evWitnessExtras(evRelPath string, evPbCC VFS) []includeDirective {
	out := make([]includeDirective, 0,
		3+len(pbDescriptorImporterHeaders)+len(evExtraProtobufHeaders)+len(evAbseilCleanupHeaders))
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(pbWrapperVFS.Rel())})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(pbDescriptorVFS.Rel())})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(evRelPath)})
	out = append(out, includeDirective{kind: includeQuoted, target: internStr(evPbCC.Rel())})

	for _, v := range pbDescriptorImporterHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: internStr(v.Rel())})
	}

	for _, v := range evExtraProtobufHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: internStr(v.Rel())})
	}

	for _, v := range evAbseilCleanupHeaders {
		out = append(out, includeDirective{kind: includeQuoted, target: internStr(v.Rel())})
	}

	return out
}

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

	evCC := Build(evRelPath + ".pb.cc")
	evH := Build(evRelPath + ".pb.h")
	srcVFS := Source(evRelPath)

	cmdArgs := []ANY{
		internAny(instance.Platform.Tools.Python3),
		internAny(pbWrapperPath),
		anyOutputs,
		vfsAny(evCC),
		vfsAny(evH),
		any2,
		vfsAny(protocBinary),
		anyI2,
		anyIS2,
		anyIB2,
		anyIS3,
		anyISContribLibsProtobufSrc,
		anyIB2,
		anyISContribLibsProtobufSrc,
		anyCppOutB,
		anyCppStyleguideOutB,
		internAny("--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary.String()),
		internAny(evRelPath),
		internAny("--plugin=protoc-gen-event2cpp=" + event2cppBinary.String()),
		anyEvent2cppOutB,
		internAny("-I=" + evEventlogIncludePath),
	}

	env := EnvVars{{Name: "ARCADIA_ROOT_DISTBUILD", Value: "$(S)"}}

	inputs := []VFS{
		cppStyleguideBinary,
		protocBinary,
		event2cppBinary,
		pbWrapperVFS,
		srcVFS,
	}

	inputs = append(inputs, transitiveImports...)

	targetProps := TargetProperties{ModuleDir: moduleDir}

	if moduleTag != nil {
		targetProps.ModuleTag = *moduleTag
	}

	var depRefs []NodeRef
	var foreignDepRefs []NodeRef

	{
		var toolRefs []NodeRef

		if cppStyleguideLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, cppStyleguideLDRef)
		}

		if protocLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, protocLDRef)
		}

		if event2cppLDRef != (NodeRef(0)) {
			toolRefs = append(toolRefs, event2cppLDRef)
		}

		if len(toolRefs) > 0 {
			depRefs = append([]NodeRef(nil), toolRefs...)
			foreignDepRefs = toolRefs
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
		Env:              env,
		Inputs:           inputs,
		Outputs:          []VFS{evCC, evH},
		KV:               KV{P: pkEV, PC: pcYellow},
		Tags:             instance.Platform.Tags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		Requirements:     Requirements{CPU: float64(1), Network: "restricted", RAM: float64(32)},
		DepRefs:          depRefs,
		ForeignDepRefs:   foreignDepRefs,
	}

	return emit.Emit(bindNodePlatform(withResources(node, resourcePatternYMakePython3), instance.Platform))
}
