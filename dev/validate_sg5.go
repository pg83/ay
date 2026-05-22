package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

type sg5Cmd struct {
	CmdArgs []string          `json:"cmd_args"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

type sg5Node struct {
	Cmds             []sg5Cmd          `json:"cmds"`
	Env              map[string]string `json:"env"`
	HostPlatform     bool              `json:"host_platform,omitempty"`
	Inputs           []string          `json:"inputs"`
	KV               map[string]string `json:"kv"`
	Outputs          []string          `json:"outputs"`
	Platform         string            `json:"platform"`
	Sandboxing       bool              `json:"sandboxing"`
	StatsUID         string            `json:"stats_uid"`
	TargetProperties map[string]string `json:"target_properties"`
}

type sg5ConfResource struct {
	Name      string               `json:"name,omitempty"`
	Pattern   string               `json:"pattern"`
	Resource  string               `json:"resource,omitempty"`
	Resources []sg5ConfResourceURI `json:"resources,omitempty"`
}

type sg5ConfResourceURI struct {
	Platform string `json:"platform"`
	Resource string `json:"resource"`
}

type sg5Capture struct {
	Resources []sg5ConfResource
	Nodes     map[string]sg5Node
}

var (
	statsUIDRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

	normalizeSpecialRoots = strings.NewReplacer(
		"$(BUILD_ROOT)", "$(B)",
		"$(SOURCE_ROOT)", "$(S)",
	)

	sg5YdbdRootOutput = "$(B)/ydb/apps/ydbd/ydbd"

	sg5NodeOutputs = map[string][]string{
		"runtime-py3-pyc": {
			"$(B)/library/python/runtime_py3/__res.pyc",
			"$(B)/library/python/runtime_py3/sitecustomize.pyc",
		},
		"grpc-upb-descriptor": {
			"$(B)/contrib/libs/grpc/third_party/upb/__/__/src/core/ext/upb-gen/google/protobuf/descriptor.upb_minitable.c.pic.o",
		},
	}
)

func main() {
	ourPath := flag.String("our", "", "path to ay sg5 raw graph")
	refPath := flag.String("ref", "", "path to upstream sg5 raw graph")
	flag.Parse()

	if *ourPath == "" || *refPath == "" {
		die("usage: validate_sg5.go --our OUR.json --ref REF.json")
	}

	our, err := loadSG5Capture(*ourPath)
	if err != nil {
		die(err.Error())
	}

	ref, err := loadSG5Capture(*refPath)
	if err != nil {
		die(err.Error())
	}

	if err := compareResources(our.Resources, ref.Resources); err != nil {
		die(err.Error())
	}

	if err := compareYdbdRootTokens(our.Nodes["ydbd-root"], ref.Nodes["ydbd-root"]); err != nil {
		die(err.Error())
	}

	if err := compareRuntimePy3Node(our.Nodes["runtime-py3-pyc"], ref.Nodes["runtime-py3-pyc"]); err != nil {
		die(err.Error())
	}

	if err := compareGrpcUpbDescriptorNode(our.Nodes["grpc-upb-descriptor"], ref.Nodes["grpc-upb-descriptor"]); err != nil {
		die(err.Error())
	}
}

func loadSG5Capture(path string) (*sg5Capture, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)

	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("read opening token from %s: %w", path, err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("unexpected opening token in %s: %v", path, tok)
	}

	capture := &sg5Capture{
		Nodes: make(map[string]sg5Node, len(sg5NodeOutputs)),
	}

	outputOwners := make(map[string]string, len(sg5NodeOutputs))
	for name, outputs := range sg5NodeOutputs {
		outputOwners[joinOutputs(outputs)] = name
	}

	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("read object key from %s: %w", path, err)
		}

		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected key token in %s: %v", path, keyTok)
		}

		switch key {
		case "conf":
			var conf struct {
				Resources []sg5ConfResource `json:"resources"`
			}
			if err := dec.Decode(&conf); err != nil {
				return nil, fmt.Errorf("decode conf from %s: %w", path, err)
			}
			capture.Resources = normalizeResources(conf.Resources)
		case "graph":
			tok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("read graph opener from %s: %w", path, err)
			}
			if d, ok := tok.(json.Delim); !ok || d != '[' {
				return nil, fmt.Errorf("unexpected graph opener in %s: %v", path, tok)
			}

			for dec.More() {
				var node sg5Node
				if err := dec.Decode(&node); err != nil {
					return nil, fmt.Errorf("decode graph node from %s: %w", path, err)
				}
				normalizeNode(&node)

				if owner, ok := outputOwners[joinOutputs(node.Outputs)]; ok {
					capture.Nodes[owner] = node
				}
				if len(node.Outputs) > 0 && node.Outputs[0] == sg5YdbdRootOutput {
					capture.Nodes["ydbd-root"] = node
				}
			}

			tok, err = dec.Token()
			if err != nil {
				return nil, fmt.Errorf("read graph closer from %s: %w", path, err)
			}
			if d, ok := tok.(json.Delim); !ok || d != ']' {
				return nil, fmt.Errorf("unexpected graph closer in %s: %v", path, tok)
			}
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, fmt.Errorf("skip %q from %s: %w", key, path, err)
			}
		}
	}

	for name := range sg5NodeOutputs {
		if _, ok := capture.Nodes[name]; !ok {
			return nil, fmt.Errorf("%s: required node %q was not found", path, name)
		}
	}
	if _, ok := capture.Nodes["ydbd-root"]; !ok {
		return nil, fmt.Errorf("%s: required node %q was not found", path, "ydbd-root")
	}

	return capture, nil
}

func normalizeResources(in []sg5ConfResource) []sg5ConfResource {
	out := make([]sg5ConfResource, len(in))
	copy(out, in)
	for i := range out {
		out[i].Name = normalizeToken(out[i].Name)
		out[i].Pattern = normalizeToken(out[i].Pattern)
		out[i].Resource = normalizeToken(out[i].Resource)
		for j := range out[i].Resources {
			out[i].Resources[j].Platform = normalizeToken(out[i].Resources[j].Platform)
			out[i].Resources[j].Resource = normalizeToken(out[i].Resources[j].Resource)
		}
	}

	return out
}

func normalizeNode(node *sg5Node) {
	node.Inputs = normalizeStrings(node.Inputs)
	node.Outputs = normalizeStrings(node.Outputs)
	node.Env = normalizeStringMap(node.Env)
	node.KV = normalizeStringMap(node.KV)
	node.Platform = normalizeToken(node.Platform)
	for i := range node.Cmds {
		node.Cmds[i].CmdArgs = normalizeStrings(node.Cmds[i].CmdArgs)
		node.Cmds[i].Cwd = normalizeToken(node.Cmds[i].Cwd)
		node.Cmds[i].Env = normalizeStringMap(node.Cmds[i].Env)
	}
	for k, v := range node.TargetProperties {
		node.TargetProperties[k] = normalizeToken(v)
	}
}

func compareResources(our, ref []sg5ConfResource) error {
	if len(our) != len(ref) {
		return fmt.Errorf("sg5 resources length mismatch:\n  our:  %d\n  ref: %d", len(our), len(ref))
	}

	for i := range our {
		if our[i].Pattern != ref[i].Pattern || our[i].Name != ref[i].Name {
			return fmt.Errorf("sg5 resources[%d] identity mismatch:\n  our:  %#v\n  ref: %#v", i, our[i], ref[i])
		}

		if our[i].Pattern == "VCS" {
			if our[i].Resource != "base64:vcs.json:e30=" {
				return fmt.Errorf("sg5 VCS resource mismatch:\n  our:  %q\n  want: %q", our[i].Resource, "base64:vcs.json:e30=")
			}

			if !strings.HasPrefix(ref[i].Resource, "base64:vcs.json:") {
				return fmt.Errorf("sg5 ref VCS resource unexpectedly malformed: %q", ref[i].Resource)
			}

			continue
		}

		if !reflect.DeepEqual(our[i], ref[i]) {
			return fmt.Errorf("sg5 resources[%d] mismatch:\n  our:  %#v\n  ref: %#v", i, our[i], ref[i])
		}
	}

	return nil
}

func compareRuntimePy3Node(our, ref sg5Node) error {
	// Host-tool stats_uid still depends on hidden StatsTags/header shape that
	// this ticket intentionally strips from the raw-slice comparison. Keep the
	// field syntactically valid, but validate the visible non-header node slice
	// exactly instead of asserting raw equality here.
	if err := requireWellFormedStatsUID("sg5 runtime_py3", our.StatsUID, ref.StatsUID); err != nil {
		return err
	}

	if err := compareExactBool("sg5 runtime_py3 host_platform", our.HostPlatform, ref.HostPlatform); err != nil {
		return err
	}
	if err := compareExactString("sg5 runtime_py3 platform", our.Platform, ref.Platform); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 runtime_py3 kv", our.KV, ref.KV); err != nil {
		return err
	}
	if err := compareExactBool("sg5 runtime_py3 sandboxing", our.Sandboxing, ref.Sandboxing); err != nil {
		return err
	}
	if err := compareExactStringSlice("sg5 runtime_py3 outputs", our.Outputs, ref.Outputs); err != nil {
		return err
	}
	if err := compareExactStringSlice("sg5 runtime_py3 non-header inputs", nonHeaderInputs(our.Inputs), nonHeaderInputs(ref.Inputs)); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 runtime_py3 env", our.Env, ref.Env); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 runtime_py3 target_properties", our.TargetProperties, ref.TargetProperties); err != nil {
		return err
	}
	if len(our.Cmds) != 1 || len(ref.Cmds) != 1 {
		return fmt.Errorf("sg5 runtime_py3 expected exactly one cmd, got our=%d ref=%d", len(our.Cmds), len(ref.Cmds))
	}
	if err := compareExactCmd("sg5 runtime_py3 cmd", our.Cmds[0], ref.Cmds[0]); err != nil {
		return err
	}

	return nil
}

func compareYdbdRootTokens(our, ref sg5Node) error {
	ourVCSCmd, err := findCommand(our.Cmds, "$(S)/build/scripts/vcs_info.py")
	if err != nil {
		return fmt.Errorf("sg5 ydbd root our node: %w", err)
	}

	refVCSCmd, err := findCommand(ref.Cmds, "$(S)/build/scripts/vcs_info.py")
	if err != nil {
		return fmt.Errorf("sg5 ydbd root ref node: %w", err)
	}

	if err := compareExactCmd("sg5 ydbd root vcs", ourVCSCmd, refVCSCmd); err != nil {
		return err
	}

	ourCompileCmd, err := findCommand(our.Cmds, "$(B)/ydb/apps/ydbd/__vcs_version__.c.o")
	if err != nil {
		return fmt.Errorf("sg5 ydbd root our compile cmd: %w", err)
	}

	refCompileCmd, err := findCommand(ref.Cmds, "$(B)/ydb/apps/ydbd/__vcs_version__.c.o")
	if err != nil {
		return fmt.Errorf("sg5 ydbd root ref compile cmd: %w", err)
	}

	if err := compareExactString("sg5 ydbd root compile tool", firstArg(ourCompileCmd.CmdArgs), firstArg(refCompileCmd.CmdArgs)); err != nil {
		return err
	}
	if err := compareExactString("sg5 ydbd root compile -o output", argAfter(ourCompileCmd.CmdArgs, "-o"), argAfter(refCompileCmd.CmdArgs, "-o")); err != nil {
		return err
	}
	if err := compareExactString("sg5 ydbd root compile source path", secondPathAfterOutput(ourCompileCmd.CmdArgs), secondPathAfterOutput(refCompileCmd.CmdArgs)); err != nil {
		return err
	}
	if err := compareExactString("sg5 ydbd root compile cwd", ourCompileCmd.Cwd, refCompileCmd.Cwd); err != nil {
		return err
	}
	if err := compareExactString("sg5 ydbd root compile DYLD_LIBRARY_PATH", ourCompileCmd.Env["DYLD_LIBRARY_PATH"], refCompileCmd.Env["DYLD_LIBRARY_PATH"]); err != nil {
		return err
	}

	ourLinkCmd, err := findCommand(our.Cmds, "$(S)/build/scripts/link_exe.py")
	if err != nil {
		return fmt.Errorf("sg5 ydbd root our link cmd: %w", err)
	}

	refLinkCmd, err := findCommand(ref.Cmds, "$(S)/build/scripts/link_exe.py")
	if err != nil {
		return fmt.Errorf("sg5 ydbd root ref link cmd: %w", err)
	}

	if err := compareExactStringSlice("sg5 ydbd root link ticket slice", ydbdRootLinkSlice(ourLinkCmd.CmdArgs), ydbdRootLinkSlice(refLinkCmd.CmdArgs)); err != nil {
		return err
	}
	if err := compareExactString("sg5 ydbd root link cwd", ourLinkCmd.Cwd, refLinkCmd.Cwd); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 ydbd root link env", ourLinkCmd.Env, refLinkCmd.Env); err != nil {
		return err
	}

	return nil
}

func compareGrpcUpbDescriptorNode(our, ref sg5Node) error {
	if err := requireWellFormedStatsUID("sg5 grpc/upb descriptor", our.StatsUID, ref.StatsUID); err != nil {
		return err
	}

	if err := compareExactBool("sg5 grpc/upb descriptor host_platform", our.HostPlatform, ref.HostPlatform); err != nil {
		return err
	}
	if err := compareExactString("sg5 grpc/upb descriptor platform", our.Platform, ref.Platform); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 grpc/upb descriptor kv", our.KV, ref.KV); err != nil {
		return err
	}
	if err := compareExactBool("sg5 grpc/upb descriptor sandboxing", our.Sandboxing, ref.Sandboxing); err != nil {
		return err
	}
	if err := compareExactStringSlice("sg5 grpc/upb descriptor outputs", our.Outputs, ref.Outputs); err != nil {
		return err
	}
	if err := compareExactStringSlice("sg5 grpc/upb descriptor non-header inputs", nonHeaderInputs(our.Inputs), nonHeaderInputs(ref.Inputs)); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 grpc/upb descriptor env", our.Env, ref.Env); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 grpc/upb descriptor target_properties", our.TargetProperties, ref.TargetProperties); err != nil {
		return err
	}

	if len(our.Cmds) != 1 || len(ref.Cmds) != 1 {
		return fmt.Errorf("sg5 grpc/upb descriptor expected exactly one cmd, got our=%d ref=%d", len(our.Cmds), len(ref.Cmds))
	}
	if err := compareExactString("sg5 grpc/upb descriptor cmd tool", firstArg(our.Cmds[0].CmdArgs), firstArg(ref.Cmds[0].CmdArgs)); err != nil {
		return err
	}
	if err := compareExactString("sg5 grpc/upb descriptor cmd -o output", argAfter(our.Cmds[0].CmdArgs, "-o"), argAfter(ref.Cmds[0].CmdArgs, "-o")); err != nil {
		return err
	}
	if err := compareExactString("sg5 grpc/upb descriptor cmd source arg", lastArg(our.Cmds[0].CmdArgs), lastArg(ref.Cmds[0].CmdArgs)); err != nil {
		return err
	}
	if err := compareExactString("sg5 grpc/upb descriptor cmd cwd", our.Cmds[0].Cwd, ref.Cmds[0].Cwd); err != nil {
		return err
	}
	if err := compareExactStringMap("sg5 grpc/upb descriptor cmd env", our.Cmds[0].Env, ref.Cmds[0].Env); err != nil {
		return err
	}

	return nil
}

func findCommand(cmds []sg5Cmd, needle string) (sg5Cmd, error) {
	for _, cmd := range cmds {
		if slices.Contains(cmd.CmdArgs, needle) {
			return cmd, nil
		}
	}

	return sg5Cmd{}, fmt.Errorf("command containing %q not found", needle)
}

func joinOutputs(outputs []string) string {
	return strings.Join(outputs, "\x00")
}

func ydbdRootLinkSlice(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		switch {
		case arg == "$(S)/build/scripts/link_exe.py",
			arg == "$(B)/ydb/apps/ydbd/__vcs_version__.c.o",
			arg == "$(B)/ydb/apps/ydbd/main.cpp.o",
			arg == "-o",
			arg == "$(B)/ydb/apps/ydbd/ydbd":
			out = append(out, arg)
		case strings.Contains(arg, "$(YMAKE_PYTHON3"),
			strings.Contains(arg, "$(CLANG"),
			strings.Contains(arg, "$(LLD_ROOT"):
			out = append(out, arg)
		}
	}

	return out
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}

func argAfter(args []string, marker string) string {
	idx := slices.Index(args, marker)
	if idx < 0 || idx+1 >= len(args) {
		return ""
	}

	return args[idx+1]
}

func secondPathAfterOutput(args []string) string {
	idx := slices.Index(args, "-o")
	if idx < 0 || idx+2 >= len(args) {
		return ""
	}

	return args[idx+2]
}

func lastArg(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[len(args)-1]
}

func requireWellFormedStatsUID(label, our, ref string) error {
	if !statsUIDRE.MatchString(our) {
		return fmt.Errorf("%s our stats_uid is not a 32-hex value: %q", label, our)
	}
	if !statsUIDRE.MatchString(ref) {
		return fmt.Errorf("%s ref stats_uid is not a 32-hex value: %q", label, ref)
	}

	return nil
}

func compareExactCmd(label string, our, ref sg5Cmd) error {
	if err := compareExactStringSlice(label+" cmd_args", our.CmdArgs, ref.CmdArgs); err != nil {
		return err
	}
	if err := compareExactString(label+" cwd", our.Cwd, ref.Cwd); err != nil {
		return err
	}
	if err := compareExactStringMap(label+" env", our.Env, ref.Env); err != nil {
		return err
	}

	return nil
}

func compareExactBool(label string, our, ref bool) error {
	if our != ref {
		return fmt.Errorf("%s mismatch:\n  our:  %t\n  ref: %t", label, our, ref)
	}

	return nil
}

func compareExactString(label, our, ref string) error {
	if our != ref {
		return fmt.Errorf("%s mismatch:\n  our:  %q\n  ref: %q", label, our, ref)
	}

	return nil
}

func compareExactStringSlice(label string, our, ref []string) error {
	if len(our) != len(ref) {
		return fmt.Errorf("%s length mismatch:\n  our:  %d\n  ref: %d", label, len(our), len(ref))
	}

	for i := range our {
		if our[i] != ref[i] {
			return fmt.Errorf("%s mismatch at [%d]:\n  our:  %q\n  ref: %q", label, i, our[i], ref[i])
		}
	}

	return nil
}

func compareExactStringMap(label string, our, ref map[string]string) error {
	ourKeys := sortedStringKeys(our)
	refKeys := sortedStringKeys(ref)
	if !reflect.DeepEqual(ourKeys, refKeys) {
		return fmt.Errorf("%s keys mismatch:\n  our:  %#v\n  ref: %#v", label, ourKeys, refKeys)
	}

	for _, key := range ourKeys {
		if our[key] != ref[key] {
			return fmt.Errorf("%s[%q] mismatch:\n  our:  %q\n  ref: %q", label, key, our[key], ref[key])
		}
	}

	return nil
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	return keys
}

func nonHeaderInputs(inputs []string) []string {
	out := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if isHeaderLikeInput(input) {
			continue
		}
		out = append(out, input)
	}

	return out
}

func isHeaderLikeInput(path string) bool {
	for _, suffix := range []string{".h", ".hh", ".hpp", ".hxx", ".inc", ".inl", ".def"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}

func normalizeStrings(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = normalizeToken(s)
	}

	return out
}

func normalizeStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = normalizeToken(v)
	}

	return out
}

func normalizeToken(s string) string {
	return normalizeSpecialRoots.Replace(s)
}

func die(msg string) {
	fmt.Fprintln(os.Stderr, "validate_sg5:", msg)
	os.Exit(1)
}
