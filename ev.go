package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

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
	evEvent2cppBinaryVFS  = Build("tools/event2cpp/event2cpp")
	evEvent2cppBinaryPath = evEvent2cppBinaryVFS.String()

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
// evAbseilCleanupHeaders.
func evWitnessExtras(sourceRoot, evRelPath string, evPbCC VFS) []includeDirective {
	_ = sourceRoot // every .ev imports descriptor.proto transitively; no source-scan needed.

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
	sourceRoot string,
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

	// Resolve transitive imports from the .ev source file and append them.
	inputs = append(inputs, resolveEvImports(sourceRoot, evRelPath)...)

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
		KV: map[string]string{
			"p":  "EV",
			"pc": "yellow",
		},
		Tags:             instance.Platform.Tags,
		TargetProperties: targetProps,
		Platform:         string(instance.Platform.Target),
		HostPlatform:     instance.Platform.IsHost,
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

// resolveEvImports returns the deduplicated transitive import set for a
// .ev/.proto file at `<sourceRoot>/<srcRel>`: every imported file found on
// disk plus descriptor.proto when any chain transitively reaches it. Reads
// each file for `import "..."` lines and follows them relative to
// sourceRoot. events_extension.proto is the primary descriptor.proto
// chain. Legitimate disk read scoped to EV-node-emission input listing,
// not closure walks.
func resolveEvImports(sourceRoot, srcRel string) []VFS {
	visited := map[string]struct{}{}
	order := make([]VFS, 0, 8)
	descriptorAdded := false

	// Queue starting from the source's imports (not the source itself —
	// it is already in inputs from the caller).
	var walk func(rel string)
	walk = func(rel string) {
		if _, seen := visited[rel]; seen {
			return
		}

		visited[rel] = struct{}{}

		// Read the file for imports.
		absPath := filepath.Join(sourceRoot, rel)
		f, err := os.Open(absPath)

		if err != nil {
			return
		}

		var imports []string
		scanner := bufio.NewScanner(f)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			if !strings.HasPrefix(line, "import ") {
				continue
			}

			// import "path/to/file.proto";
			// Extract the quoted path.
			start := strings.IndexByte(line, '"')
			end := strings.LastIndexByte(line, '"')

			if start < 0 || end <= start {
				continue
			}

			importedRel := line[start+1 : end]
			// google/protobuf/descriptor.proto is resolved via the
			// protobuf include path, not directly under sourceRoot.
			// Detect and emit the canonical path once.
			if importedRel == "google/protobuf/descriptor.proto" {
				if !descriptorAdded {
					order = append(order, pbDescriptorVFS)
					descriptorAdded = true
				}
				continue
			}
			imports = append(imports, importedRel)
		}

		f.Close()

		// Emit this file's absolute $(S)/... entry.
		order = append(order, Source(rel))

		// Recurse into imports.
		for _, imp := range imports {
			walk(imp)
		}
	}

	// Start from the imports of the primary source file.
	absPath := filepath.Join(sourceRoot, srcRel)
	f, err := os.Open(absPath)

	if err != nil {
		return nil
	}

	var topImports []string
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if !strings.HasPrefix(line, "import ") {
			continue
		}

		start := strings.IndexByte(line, '"')
		end := strings.LastIndexByte(line, '"')

		if start < 0 || end <= start {
			continue
		}

		importedRel := line[start+1 : end]
		topImports = append(topImports, importedRel)
	}

	f.Close()

	for _, imp := range topImports {
		walk(imp)
	}

	return order
}
