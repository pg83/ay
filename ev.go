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
)

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
		Tags:             []string{},
		TargetProperties: targetProps,
		Platform:         string(instance.Target),
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
func resolveEvImports(sourceRoot, srcRel string) []string {
	visited := map[string]struct{}{}
	order := make([]string, 0, 8)

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
