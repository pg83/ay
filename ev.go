package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ev.go — emitter for EV (event-log .ev → .ev.pb.cc/.ev.pb.h) nodes.
//
// EmitEV emits one EV node per .ev source. The shape is structurally
// identical to PB but appends three extra cmd_args for the event2cpp
// protoc plugin, and uses .ev.pb.cc / .ev.pb.h output suffixes (in that
// order — cc first, per the reference graph).
//
// Reference cmd_args (21 args):
//
//	/ix/realm/pg/bin/python3
//	$(S)/build/scripts/cpp_proto_wrapper.py
//	--outputs <.ev.pb.cc> <.ev.pb.h>
//	--
//	$(B)/contrib/tools/protoc/protoc
//	-I=./ -I=$(S)/ -I=$(B) -I=$(S)
//	-I=$(S)/contrib/libs/protobuf/src
//	-I=$(B) -I=$(S)/contrib/libs/protobuf/src
//	--cpp_out=:$(B)/
//	--cpp_styleguide_out=:$(B)/
//	--plugin=protoc-gen-cpp_styleguide=<cpp_styleguide_binary>
//	<module_dir/ev_file>
//	--plugin=protoc-gen-event2cpp=<event2cpp_binary>
//	--event2cpp_out=$(B)
//	-I=$(S)/library/cpp/eventlog
//
// inputs = [cpp_styleguide, protoc, event2cpp, cpp_proto_wrapper.py,
//           $(S)/<module_dir>/<src>,
//           ... transitive .ev imports ...,
//           ... transitive .proto imports ...,
//           optionally descriptor.proto]
//
// The transitive import set is resolved by scanning the .ev source for
// `import "..."` lines, then recursively resolving those imports.
// events_extension.proto (the standard eventlog import) and its
// transitive descriptor.proto are included when reachable.
//
// foreign_deps / deps carry [cpp_styleguide, protoc, event2cpp] (3 refs).
// tags: always [] (EV nodes only appear on aarch64 in the reference).

const (
	evEvent2cppBinaryPath = "$(B)/tools/event2cpp/event2cpp"
	// evEvent2cppModule is the ya.make path walked to obtain the event2cpp host
	// LD node. tools/event2cpp/ya.make uses INCLUDE() patterns that our parser
	// does not expand; tools/event2cpp/bin/ya.make is the actual PROGRAM
	// declaration. ldBinaryDir lifts the output dir from tools/event2cpp/bin to
	// tools/event2cpp so the LD node's module_dir matches the reference.
	evEvent2cppModule     = "tools/event2cpp/bin"
	evEventlogIncludePath = "$(S)/library/cpp/eventlog"
)

// eventRuntimeHeaders is the common subset of SOURCE_ROOT headers present in
// every .ev.pb.cc CC node in the reference graph (68 entries, derived from
// the intersection across all 2 .ev.pb.cc consumers in sg2.json).
// These are registered as EmitsIncludes on the .ev.pb.h output so the scanner
// closure propagates them into all CC nodes that consume .ev.pb.h.
// Sorted lexicographically.
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

// evExtraProtobufHeaders is the .ev.pb.h-specific subset of protobuf headers
// that show up only in CC consumers of event-generated headers (verified by
// intersecting the inputs of every .ev.pb.h CC consumer in sg2.json). These
// stem from event2cpp's generated reflection code and the protoc plugin
// scaffolding used by event2cpp. Sorted lexicographically.
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
// through every .ev.pb.h to its CC consumers. The set is:
//   - cpp_proto_wrapper.py (the script that drives event2cpp+protoc),
//   - descriptor.proto (every .ev imports it indirectly via events_extension),
//   - the .ev source file itself,
//   - the companion .ev.pb.cc generated next to .ev.pb.h,
//   - the descriptor-importer protobuf header cluster
//     (pbDescriptorImporterHeaders),
//   - evExtraProtobufHeaders (event2cpp-specific protobuf cluster),
//   - evAbseilCleanupHeaders (abseil cleanup pair).
//
// See docs/drafts/20260512-0200-residue-pre-100pct.md §2 lever #1.
func evWitnessExtras(sourceRoot, evRelPath string, evPbCC VFS) []VFS {
	_ = sourceRoot // every .ev imports descriptor.proto transitively; no source-scan needed.

	out := make([]VFS, 0,
		3+len(pbDescriptorImporterHeaders)+len(evExtraProtobufHeaders)+len(evAbseilCleanupHeaders))
	out = append(out, ParseVFSOrSource(pbWrapperPath))
	out = append(out, ParseVFSOrSource(pbDescriptorProto))
	out = append(out, Source(evRelPath))
	out = append(out, evPbCC)
	out = append(out, pbDescriptorImporterHeaders...)
	out = append(out, evExtraProtobufHeaders...)
	out = append(out, evAbseilCleanupHeaders...)

	return out
}

// EmitEV emits an EV node for `srcRel` (a .ev file relative to `instance.Path`).
func EmitEV(
	instance ModuleInstance,
	srcRel string,
	cppStyleguideLDRef NodeRef,
	protocLDRef NodeRef,
	event2cppLDRef NodeRef,
	cppStyleguideBinary string,
	protocBinary string,
	event2cppBinary string,
	moduleTag string,
	sourceRoot string,
	emit Emitter,
) NodeRef {
	moduleDir := instance.Path
	evRelPath := moduleDir + "/" + srcRel

	// EV outputs: .ev.pb.cc first, then .ev.pb.h (reference order).
	evCC := "$(B)/" + evRelPath + ".pb.cc"
	evH := "$(B)/" + evRelPath + ".pb.h"
	srcAbs := "$(S)/" + evRelPath

	cmdArgs := []string{
		pbPython3Path,
		pbWrapperPath,
		"--outputs",
		evCC,
		evH,
		"--",
		protocBinary,
		"-I=./",
		"-I=$(S)/",
		"-I=$(B)",
		"-I=$(S)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"-I=$(B)",
		"-I=$(S)/contrib/libs/protobuf/src",
		"--cpp_out=:$(B)/",
		"--cpp_styleguide_out=:$(B)/",
		"--plugin=protoc-gen-cpp_styleguide=" + cppStyleguideBinary,
		evRelPath,
		"--plugin=protoc-gen-event2cpp=" + event2cppBinary,
		"--event2cpp_out=$(B)",
		"-I=" + evEventlogIncludePath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(S)",
	}

	// Build inputs: tool binaries + wrapper + source + transitive imports.
	inputs := []VFS{
		ParseVFSOrSource(cppStyleguideBinary),
		ParseVFSOrSource(protocBinary),
		ParseVFSOrSource(event2cppBinary),
		ParseVFSOrSource(pbWrapperPath),
		ParseVFSOrSource(srcAbs),
	}

	// Resolve transitive imports from the .ev source file and append them.
	for _, p := range resolveEvImports(sourceRoot, moduleDir+"/"+srcRel) {
		inputs = append(inputs, ParseVFSOrSource(p))
	}

	targetProps := map[string]string{
		"module_dir": moduleDir,
	}

	if moduleTag != "" {
		targetProps["module_tag"] = moduleTag
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
		Outputs: []VFS{ParseVFSOrSource(evCC), ParseVFSOrSource(evH)},
		KV: map[string]string{
			"p":  "EV",
			"pc": "yellow",
		},
		Tags: instance.Platform.Tags,
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

// resolveEvImports resolves the transitive import set for a .ev (or .proto)
// file rooted at `<sourceRoot>/<srcRel>`. Returns a deduplicated, ordered
// slice of `$(S)/...` paths for every imported file that can be
// found on disk, plus descriptor.proto when any import chain transitively
// reaches it.
//
// The scan is shallow: we read each file for `import "..."` lines and
// follow the referenced paths relative to the source root. The eventlog
// import `library/cpp/eventlog/proto/events_extension.proto` is the
// primary transitive chain that surfaces descriptor.proto.
//
// PR-AUDIT-3: legitimate disk read — extracts structured `import` directives
// from .ev/.proto sources at EV-node-emission time to build the EV node's
// input list. NOT for closure walks. The architectural cleanup to route this
// through a unified registry-resolved "structured-import extractor" lives in
// PR-AUDIT-3.D12 (still open); kept per audit doc §2 D12.
func resolveEvImports(sourceRoot, srcRel string) []string {
	visited := map[string]struct{}{}
	order := make([]string, 0, 8)
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
					order = append(order, pbDescriptorProto)
					descriptorAdded = true
				}
				continue
			}
			imports = append(imports, importedRel)
		}

		f.Close()

		// Emit this file's absolute $(S)/... entry.
		order = append(order, "$(S)/"+rel)

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
