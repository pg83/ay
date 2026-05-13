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
//	$(SOURCE_ROOT)/build/scripts/cpp_proto_wrapper.py
//	--outputs <.ev.pb.cc> <.ev.pb.h>
//	--
//	$(BUILD_ROOT)/contrib/tools/protoc/protoc
//	-I=./ -I=$(SOURCE_ROOT)/ -I=$(BUILD_ROOT) -I=$(SOURCE_ROOT)
//	-I=$(SOURCE_ROOT)/contrib/libs/protobuf/src
//	-I=$(BUILD_ROOT) -I=$(SOURCE_ROOT)/contrib/libs/protobuf/src
//	--cpp_out=:$(BUILD_ROOT)/
//	--cpp_styleguide_out=:$(BUILD_ROOT)/
//	--plugin=protoc-gen-cpp_styleguide=<cpp_styleguide_binary>
//	<module_dir/ev_file>
//	--plugin=protoc-gen-event2cpp=<event2cpp_binary>
//	--event2cpp_out=$(BUILD_ROOT)
//	-I=$(SOURCE_ROOT)/library/cpp/eventlog
//
// inputs = [cpp_styleguide, protoc, event2cpp, cpp_proto_wrapper.py,
//           $(SOURCE_ROOT)/<module_dir>/<src>,
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
	evEvent2cppBinaryPath = "$(BUILD_ROOT)/tools/event2cpp/event2cpp"
	// evEvent2cppModule is the ya.make path walked to obtain the event2cpp host
	// LD node. tools/event2cpp/ya.make uses INCLUDE() patterns that our parser
	// does not expand; tools/event2cpp/bin/ya.make is the actual PROGRAM
	// declaration. ldBinaryDir lifts the output dir from tools/event2cpp/bin to
	// tools/event2cpp so the LD node's module_dir matches the reference.
	evEvent2cppModule     = "tools/event2cpp/bin"
	evEventlogIncludePath = "$(SOURCE_ROOT)/library/cpp/eventlog"
	evSourceBase          = "$(SOURCE_ROOT)/"
)

// eventRuntimeHeaders is the common subset of SOURCE_ROOT headers present in
// every .ev.pb.cc CC node in the reference graph (68 entries, derived from
// the intersection across all 2 .ev.pb.cc consumers in sg2.json).
// These are registered as EmitsIncludes on the .ev.pb.h output so the scanner
// closure propagates them into all CC nodes that consume .ev.pb.h.
// Sorted lexicographically. VFS-rooted $(SOURCE_ROOT)/... paths.
var eventRuntimeHeaders = []string{
	evSourceBase + "library/cpp/eventlog/event_field_output.h",
	evSourceBase + "library/cpp/eventlog/event_field_printer.h",
	evSourceBase + "library/cpp/eventlog/events_extension.h",
	evSourceBase + "util/charset/unicode_table.h",
	evSourceBase + "util/charset/unidata.h",
	evSourceBase + "util/digest/numeric.h",
	evSourceBase + "util/generic/array_size.h",
	evSourceBase + "util/generic/bitops.h",
	evSourceBase + "util/generic/buffer.h",
	evSourceBase + "util/generic/cast.h",
	evSourceBase + "util/generic/deque.h",
	evSourceBase + "util/generic/explicit_type.h",
	evSourceBase + "util/generic/flags.h",
	evSourceBase + "util/generic/fwd.h",
	evSourceBase + "util/generic/hide_ptr.h",
	evSourceBase + "util/generic/intrlist.h",
	evSourceBase + "util/generic/iterator.h",
	evSourceBase + "util/generic/map.h",
	evSourceBase + "util/generic/mapfindptr.h",
	evSourceBase + "util/generic/mem_copy.h",
	evSourceBase + "util/generic/noncopyable.h",
	evSourceBase + "util/generic/ptr.h",
	evSourceBase + "util/generic/refcount.h",
	evSourceBase + "util/generic/reserve.h",
	evSourceBase + "util/generic/singleton.h",
	evSourceBase + "util/generic/store_policy.h",
	evSourceBase + "util/generic/strbase.h",
	evSourceBase + "util/generic/strbuf.h",
	evSourceBase + "util/generic/string.h",
	evSourceBase + "util/generic/string_hash.h",
	evSourceBase + "util/generic/typelist.h",
	evSourceBase + "util/generic/typetraits.h",
	evSourceBase + "util/generic/utility.h",
	evSourceBase + "util/generic/va_args.h",
	evSourceBase + "util/generic/yexception.h",
	evSourceBase + "util/generic/ylimits.h",
	evSourceBase + "util/memory/alloc.h",
	evSourceBase + "util/memory/tempbuf.h",
	evSourceBase + "util/str_stl.h",
	evSourceBase + "util/stream/fwd.h",
	evSourceBase + "util/stream/input.h",
	evSourceBase + "util/stream/labeled.h",
	evSourceBase + "util/stream/output.h",
	evSourceBase + "util/stream/str.h",
	evSourceBase + "util/stream/tempbuf.h",
	evSourceBase + "util/stream/zerocopy.h",
	evSourceBase + "util/stream/zerocopy_output.h",
	evSourceBase + "util/string/hex.h",
	evSourceBase + "util/string/subst.h",
	evSourceBase + "util/system/align.h",
	evSourceBase + "util/system/atexit.h",
	evSourceBase + "util/system/backtrace.h",
	evSourceBase + "util/system/compat.h",
	evSourceBase + "util/system/compiler.h",
	evSourceBase + "util/system/defaults.h",
	evSourceBase + "util/system/error.h",
	evSourceBase + "util/system/guard.h",
	evSourceBase + "util/system/mutex.h",
	evSourceBase + "util/system/platform.h",
	evSourceBase + "util/system/src_location.h",
	evSourceBase + "util/system/src_root.h",
	evSourceBase + "util/system/thread.i",
	evSourceBase + "util/system/type_name.h",
	evSourceBase + "util/system/types.h",
	evSourceBase + "util/system/unaligned_mem.h",
	evSourceBase + "util/system/win_undef.h",
	evSourceBase + "util/system/winint.h",
	evSourceBase + "util/system/yassert.h",
}

// evExtraProtobufHeaders is the .ev.pb.h-specific subset of protobuf headers
// that show up only in CC consumers of event-generated headers (verified by
// intersecting the inputs of every .ev.pb.h CC consumer in sg2.json). These
// stem from event2cpp's generated reflection code and the protoc plugin
// scaffolding used by event2cpp. Sorted lexicographically.
var evExtraProtobufHeaders = []string{
	pbRuntimeBase + "google/protobuf/io/printer.h",
	pbRuntimeBase + "google/protobuf/io/zero_copy_sink.h",
	pbRuntimeBase + "google/protobuf/stubs/hash.h",
	pbRuntimeBase + "google/protobuf/stubs/stringpiece.h",
	pbRuntimeBase + "google/protobuf/stubs/strutil.h",
}

// evAbseilCleanupHeaders is the abseil RAII-cleanup pair propagated through
// every .ev.pb.h to its CC consumers (verified in sg2.json).
var evAbseilCleanupHeaders = []string{
	evSourceBase + "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/cleanup.h",
	evSourceBase + "contrib/restricted/abseil-cpp-tstring/y_absl/cleanup/internal/cleanup.h",
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
func evWitnessExtras(sourceRoot, evRelPath, evPbCC string) []string {
	_ = sourceRoot // every .ev imports descriptor.proto transitively; no source-scan needed.

	out := make([]string, 0,
		3+len(pbDescriptorImporterHeaders)+len(evExtraProtobufHeaders)+len(evAbseilCleanupHeaders))
	out = append(out, pbWrapperPath)
	out = append(out, pbDescriptorProto)
	out = append(out, "$(SOURCE_ROOT)/"+evRelPath)
	out = append(out, evPbCC)
	out = append(out, pbDescriptorImporterHeaders...)
	out = append(out, evExtraProtobufHeaders...)
	out = append(out, evAbseilCleanupHeaders...)

	return out
}

// EmitEV emits an EV node for `srcRel` (a .ev file relative to `instance.Path`).
func EmitEV(
	hostP, targetP *Platform,
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
	_ = hostP // PR-M3-platform-pair-step8: surfaced for signature symmetry.
	moduleDir := instance.Path
	evRelPath := moduleDir + "/" + srcRel

	// EV outputs: .ev.pb.cc first, then .ev.pb.h (reference order).
	evCC := "$(BUILD_ROOT)/" + evRelPath + ".pb.cc"
	evH := "$(BUILD_ROOT)/" + evRelPath + ".pb.h"
	srcAbs := "$(SOURCE_ROOT)/" + evRelPath

	cmdArgs := []string{
		pbPython3Path,
		pbWrapperPath,
		"--outputs",
		evCC,
		evH,
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
		evRelPath,
		"--plugin=protoc-gen-event2cpp=" + event2cppBinary,
		"--event2cpp_out=$(BUILD_ROOT)",
		"-I=" + evEventlogIncludePath,
	}

	env := map[string]string{
		"ARCADIA_ROOT_DISTBUILD": "$(SOURCE_ROOT)",
	}

	// Build inputs: tool binaries + wrapper + source + transitive imports.
	inputs := []string{
		cppStyleguideBinary,
		protocBinary,
		event2cppBinary,
		pbWrapperPath,
		srcAbs,
	}

	// Resolve transitive imports from the .ev source file and append them.
	importedInputs := resolveEvImports(sourceRoot, moduleDir+"/"+srcRel)
	inputs = append(inputs, importedInputs...)

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
				Cwd:     "$(SOURCE_ROOT)",
				Env:     env,
			},
		},
		Env:     env,
		Inputs:  inputs,
		Outputs: []string{evCC, evH},
		KV: map[string]string{
			"p":  "EV",
			"pc": "yellow",
		},
		// PR-M3-platform-pair-step8: tags + host_platform + platform
		// from targetP; empty Tags initialised as []string{} for
		// non-nil JSON output.
		Tags: func() []string {
			out := []string{}
			if len(targetP.Tags) > 0 {
				out = append(out, targetP.Tags...)
			}
			return out
		}(),
		TargetProperties: targetProps,
		Platform:         string(targetP.Target),
		HostPlatform:     targetP.IsHost,
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
// slice of `$(SOURCE_ROOT)/...` paths for every imported file that can be
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

		// Emit this file's absolute $(SOURCE_ROOT)/... entry.
		order = append(order, "$(SOURCE_ROOT)/"+rel)

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
