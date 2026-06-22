package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	json "github.com/goccy/go-json"
)

var (
	// versionedResourceRe matches a resource reference carrying a sandbox/content
	// id suffix, e.g. $(CLANG16-1380963495) or $(TEST_TOOL_HOST-sbr:12302016680).
	// The resource name is an uppercase identifier that MAY contain digits
	// (CLANG16, CLANG20, JDK17), so the name class allows [A-Z0-9_] after the
	// first letter; the id is bare digits or an `sbr:<digits>` reference.
	// Normalization strips the id suffix on both graphs so versioned and bare
	// references compare equal — these ids churn whenever upstream rotates a
	// sandbox resource and carry no build-graph meaning.
	versionedResourceRe = regexp.MustCompile(`\$\(([A-Z][A-Z0-9_]*)-(?:sbr:)?[0-9]+\)`)
	// resourceMountFoldRe folds our self-contained resource path back to upstream's
	// bare reference form: we emit the toolchain as $(B)/resources/<NAME>/bin/… (a
	// real FETCH-node output the consumer depends on), upstream emits $(<NAME>)/bin/…
	// (an executor-resolved mount). Folding $(B)/resources/<NAME> -> $(<NAME>) makes
	// the two compare equal after the FETCH node itself is stripped.
	resourceMountFoldRe = regexp.MustCompile(`\$\(B\)/resources/([A-Z_][A-Z0-9_]*)`)
	// cmdLiteralBasenames are bare-word command arguments that collide with a real
	// input's basename but do NOT name that file: "gnu" is the llvm-ar archive-format
	// selector (link_lib.py ... LLVM_AR gnu $(B) ...), not the magic-database source
	// contrib/libs/libmagic/magic/Magdir/gnu. An input whose basename is one of these
	// is never kept via the command-name rule.
	cmdLiteralBasenames = map[string]bool{"gnu": true}
)

// ldOwnScriptRels is the reference-graph AR/LD input whitelist: the build/scripts
// tooling (and the vcs templates) a link node legitimately carries but never names
// on its command line, so reference-graph pruning keeps them. This is the FULL set
// including the wrappers' import closures (link_exe imports process_command_files,
// thinlto_cache, process_whole_archive_option; fs_tools imports
// process_command_files). The normalizer is a standalone tool with no access to
// build/scripts, so unlike the generator (which derives closures from the script
// table) it must list them explicitly.
var ldOwnScriptRels = map[string]bool{
	"build/scripts/link_dyn_lib.py":                 true,
	"build/scripts/link_exe.py":                     true,
	"build/scripts/process_command_files.py":        true,
	"build/scripts/process_whole_archive_option.py": true,
	"build/scripts/thinlto_cache.py":                true,
	"build/scripts/fs_tools.py":                     true,
	"build/scripts/vcs_info.py":                     true,
	"build/scripts/c_templates/svn_interface.c":     true,
	"build/scripts/c_templates/svnversion.h":        true,
}

var dumpContentFields = []string{
	"cmds", "env", "inputs", "kv", "outputs",
	"platform", "requirements", "target_properties",
}

// objcopyOverEmitExts are the C/C++ source/header extensions that constitute the
// embedded resource producer's leaked compile closure on a resource-objcopy node.
var objcopyOverEmitExts = map[string]struct{}{
	".h": {}, ".hpp": {}, ".hxx": {}, ".ipp": {}, ".inc": {}, ".def": {},
	".proto": {}, ".cpp": {}, ".cc": {}, ".cxx": {}, ".c": {}, ".i": {}, ".td": {},
	".txt": {}, // COPY_FILE(TEXT) header sources, e.g. mkql_computation_node_codegen.h.txt
}

func normPath(s string) string {
	if !strings.Contains(s, "$(") {
		return s
	}

	s = strings.ReplaceAll(s, "$(BUILD_ROOT)", "$(B)")
	s = strings.ReplaceAll(s, "$(SOURCE_ROOT)", "$(S)")

	// Fold our $(B)/resources/<NAME>/… toolchain paths back to the bare $(<NAME>)/…
	// form upstream emits (the FETCH node that produces them is stripped separately).
	if strings.Contains(s, "$(B)/resources/") {
		// The toolchain compiler is our version-specific CLANG20 resource, but
		// upstream INVOKES it through the version-independent bare $(CLANG). Map the
		// tool paths (CLANG20/bin/…, CLANG20/lib) — note the trailing slash, so the
		// bare CLANG20_RESOURCE_GLOBAL::$(B)/resources/CLANG20 declaration value is NOT
		// caught here and keeps its versioned $(CLANG20) via the general fold below
		// (upstream declares the resource global versioned, invokes the tool bare).
		s = strings.ReplaceAll(s, "$(B)/resources/CLANG20/", "$(CLANG)/")
		s = resourceMountFoldRe.ReplaceAllString(s, "$($1)")
	}

	// Our inline vcs.json is a real $(B)/vcs.json producer node; upstream mounts it as
	// $(VCS)/vcs.json with no producer. Fold the reference (the producer is stripped).
	s = strings.ReplaceAll(s, "$(B)/vcs.json", "$(VCS)/vcs.json")

	// Strip the id suffix from any versioned resource ref ($(NAME-id) /
	// $(NAME-sbr:id)). The regex matches only refs carrying an id, so running it
	// on every $(-bearing string is safe and covers all resources (CLANG*,
	// LLD_ROOT, YMAKE_PYTHON3, TEST_TOOL_HOST, JDK17, …) without an allow-list.
	if strings.Contains(s, "-") {
		s = versionedResourceRe.ReplaceAllStringFunc(s, func(m string) string {
			return "$(" + versionedResourceRe.FindStringSubmatch(m)[1] + ")"
		})
	}

	return s
}

func normRec(v any) any {
	switch t := v.(type) {
	case string:
		return normPath(t)
	case []any:
		for i := range t {
			t[i] = normRec(t[i])
		}

		return t
	case map[string]any:
		for k := range t {
			t[k] = normRec(t[k])
		}

		return t
	default:
		return v
	}
}

func toStrings(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))

	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}

	return out
}

func normSortedStrings(in []string) []string {
	out := make([]string, len(in))

	for i := range in {
		out[i] = normPath(in[i])
	}

	sort.Strings(out)
	// Dedup adjacent: a node may list the same input more than once — e.g. several
	// wrapper scripts pulling in a shared helper (link_exe and fs_tools both import
	// process_command_files) — but the canonical form is a set.
	w := 0

	for i := range out {
		if i == 0 || out[i] != out[w-1] {
			out[w] = out[i]
			w++
		}
	}

	return out[:w]
}

func normStringsKeepOrder(in []string) []string {
	out := make([]string, len(in))

	for i := range in {
		out[i] = normPath(in[i])
	}

	return out
}

func orVal(v, def any) any {
	if v == nil {
		return def
	}

	return v
}

func nodeProgramKind(node *rawNode) string {
	kv, _ := node.Kv.(map[string]any)
	p, _ := kv["p"].(string)

	return p
}

// nodeCmdText flattens a node's command arguments into one NUL-joined, normPath'd
// string for whole-path substring testing (used by the dep strip to detect a dep
// output the command names directly, e.g. a TS run_test --context path that is not
// listed among the node's inputs).
func nodeCmdText(node *rawNode) string {
	cmds, _ := node.Cmds.([]any)
	var b strings.Builder

	for _, c := range cmds {
		m, _ := c.(map[string]any)

		for _, a := range toStrings(m["cmd_args"]) {
			b.WriteString(normPath(a))
			b.WriteByte('\x00')
		}
	}

	return b.String()
}

// nodeCmdBasenames returns the set of file basenames named by a node's command
// arguments. Each cmd_arg token contributes its basename (the part after the last
// '/'); a trailing ':' (the resource archiver's "path:" syntax) is stripped.
// The match is on whole basenames, not substrings, so a source input (foo.c) is
// not spuriously matched against the object token that embeds its name (foo.c.o).
//
// The archiver's `-k $KEYS` value (ARCHIVE_BY_KEYS, ymake.core.conf) is a
// colon-joined list of archive *keys*, not input file paths; the archiver
// addresses entries by name and reads no file for it. Its trailing key would
// otherwise leak a phantom file basename (e.g. tests/ft/yabs_md5.lua), spuriously
// keeping a source member that ymake over-emits as a cache-key input but the
// command never actually names — so skip the token following -k.
func nodeCmdBasenames(node *rawNode) map[string]struct{} {
	cmds, _ := node.Cmds.([]any)
	set := map[string]struct{}{}

	for _, c := range cmds {
		m, _ := c.(map[string]any)
		args := toStrings(m["cmd_args"])

		for i, a := range args {
			if i > 0 && args[i-1] == "-k" {
				continue
			}

			b := strings.TrimRight(baseName(normPath(a)), ":")
			set[b] = struct{}{}
		}
	}

	return set
}

// arLDInputKept decides whether a link/archive input survives reference-graph
// pruning. Keep it if it is a real build artifact / known script (the white
// list), OR if the command names its file — its basename matches a whole basename
// token of the command (cmdBases). ymake propagates the full transitive
// header/source set onto AR/LD nodes as inputs; those are never named by the
// link/ar command (the .o/.a go through a response file, headers/protos aren't
// passed at all), so they fall away — while genuinely-consumed inputs the command
// DOES name (vcs svnversion.h, llvm-link .bc, exports.symlist, the archiver tool,
// archived .pyc) are kept.
func arLDInputKept(s, kind string, cmdBases map[string]struct{}) bool {
	if strings.HasSuffix(s, ".o") || strings.HasSuffix(s, ".a") ||
		strings.HasSuffix(s, ".pyplugin") || strings.HasSuffix(s, ".exports") {
		return true
	}

	if rel, ok := strings.CutPrefix(s, "$(S)/"); ok {
		if kind == "AR" && rel == "build/scripts/link_lib.py" {
			return true
		}

		if kind == "LD" && ldOwnScriptRels[rel] {
			return true
		}
	}

	b := baseName(s)

	if cmdLiteralBasenames[b] {
		return false
	}

	_, ok := cmdBases[b]

	return ok
}

func baseName(s string) string {
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}

	return s
}

func filterARLDInputs(in []string, kind string, cmdBases map[string]struct{}) []string {
	out := in[:0]

	for _, s := range in {
		if arLDInputKept(s, kind, cmdBases) {
			out = append(out, s)
		}
	}

	return out
}

// filterObjcopyInputs drops the embedded resource producer's transitive $(S)
// compile/source closure that upstream over-emits onto a resource-objcopy node as
// cache-key-only inputs (the objcopy.py action reads only the resource it embeds;
// none of these appear in cmd_args). Classified per file from the our-vs-ref input
// delta (see bugs/20260615-upstream-resource-objcopy-overemit.md): the closure is
// everything under $(S)/contrib/libs/ (libcxx/libmagic/clang-rt/protobuf headers &
// data), every C/C++ source/header by extension (module-local generated .pb.h /
// .proto included), and the $(S)/build/ generator wrappers (cpp_proto_wrapper.py).
// Command-named files (the resource, objcopy.py, the tools), $(B) artifacts, the
// python3 interpreter stdlib and contrib/python package sources are kept.
func filterObjcopyInputs(in []string, cmdBases map[string]struct{}) []string {
	out := in[:0]

	for _, s := range in {
		if objcopyInputKept(s, cmdBases) {
			out = append(out, s)
		}
	}

	return out
}

func objcopyInputKept(s string, cmdBases map[string]struct{}) bool {
	b := baseName(s)

	if _, named := cmdBases[b]; named && !cmdLiteralBasenames[b] {
		return true
	}

	rel, isSrc := strings.CutPrefix(s, "$(S)/")

	if !isSrc {
		return true
	}

	return objcopySourceLeafKept(rel)
}

// objcopySourceLeafKept reports whether a module-relative $(S) source leaf
// survives the resource-objcopy input over-emit prune. Upstream lists an embedded
// generated payload's producer's full transitive $(S) compile closure on the
// objcopy node, but only genuine data-source leaves are meaningful: the C/C++
// compile noise (headers, sources, .proto/.txt — objcopyOverEmitExts), contrib/libs
// sources, and build/*.py tooling are discounted. The faithful (unfiltered) graph
// side emits exactly this kept set so resource source-attribution matches
// ref-after-normalization. Shared by objcopyInputKept (refGraph prune) and
// emit_py_objcopy.go's source-attribution tail.
func objcopySourceLeafKept(rel string) bool {
	b := baseName(rel)

	if strings.HasPrefix(rel, "contrib/libs/") {
		return false
	}

	if _, over := objcopyOverEmitExts[fileExt(b)]; over {
		return false
	}

	if strings.HasPrefix(rel, "build/") && strings.HasSuffix(b, ".py") {
		return false
	}

	return true
}

func fileExt(base string) string {
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i:]
	}

	return ""
}

func getString(node map[string]any, key string) string {
	s, _ := node[key].(string)

	return s
}

// canonInputs returns a node's inputs after canonical filtering. filterARLDInputs
// (AR/LD input pruning) is applied ONLY to the upstream reference graph
// (refGraph) — it discounts inputs ymake lists on link/archive nodes that our
// generator does not (or should not) model. Our graph is normalized faithfully so
// that any genuine over- or under-emission surfaces as a diff, and so the filter
// can be tightened to drop only what is really superfluous.
func canonInputs(node *rawNode, refGraph bool) []string {
	inputs := normSortedStrings(node.Inputs)

	if !refGraph {
		return inputs
	}

	kind := nodeProgramKind(node)

	switch {
	case kind == "AR" || kind == "LD":
		inputs = filterARLDInputs(inputs, kind, nodeCmdBasenames(node))
	case kind == "PY":
		// Resource-embedding objcopy (build/scripts/objcopy.py): upstream lists
		// the embedded resource's producer's full transitive $(S) compile closure
		// (libcxx/sanitizer headers, cpp_proto_wrapper.py, …) as inputs, though
		// objcopy.py reads only the resource it embeds — those paths never appear
		// in cmd_args. Same over-emit class as AR/LD; keep only command-named
		// inputs. See bugs/20260615-upstream-resource-objcopy-overemit.md.
		cmdBases := nodeCmdBasenames(node)

		if _, ok := cmdBases["objcopy.py"]; ok {
			inputs = filterObjcopyInputs(inputs, cmdBases)
		}
	}

	return inputs
}

func canonContent(node *rawNode, refGraph bool) map[string]any {
	inputs := canonInputs(node, refGraph)

	canon := map[string]any{
		"cmds":              normRec(orVal(node.Cmds, []any{})),
		"env":               normRec(orVal(node.Env, map[string]any{})),
		"inputs":            inputs,
		"kv":                normRec(orVal(node.Kv, map[string]any{})),
		"outputs":           normStringsKeepOrder(node.Outputs),
		"platform":          normPath(node.Platform),
		"requirements":      normRec(orVal(node.Requirements, map[string]any{})),
		"sandboxing":        true,
		"target_properties": normRec(orVal(node.TargetProperties, map[string]any{})),
	}

	// tags and host_platform are a graph-merge artifact (devtools/ya assigns the
	// "tool" tag / host_platform to host-contour nodes at merge time); they are not
	// intrinsic to a node's build action. Strip them from both sides so a host
	// (tool) instance and its target twin compare on their real content.
	return canon
}

func marshalCompact(v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	throw(enc.Encode(v))

	b := buf.Bytes()

	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}

	return b
}

// rawNode is the typed decode target for a raw graph node on the normalize hot
// path (streamGraphFanout). The large string-array fields (deps/inputs/outputs)
// and scalars (uid/platform) decode directly to their Go types, avoiding the
// per-element interface boxing of a []any decode and the toStrings copy that
// followed it. The fields canonContent feeds to normRec (cmds/env/kv/
// requirements/target_properties) stay any so the canonicalized value tree — and
// thus marshalCompact's bytes — is identical to the former map[string]any decode;
// keeping them any also preserves orVal's `v == nil` substitution for absent
// fields (a typed-nil map wrapped in any is != nil and would emit null, not {}).
type rawNode struct {
	UID              string   `json:"uid"`
	Deps             []string `json:"deps"`
	Inputs           []string `json:"inputs"`
	Outputs          []string `json:"outputs"`
	Platform         string   `json:"platform"`
	Cmds             any      `json:"cmds"`
	Env              any      `json:"env"`
	Kv               any      `json:"kv"`
	Requirements     any      `json:"requirements"`
	TargetProperties any      `json:"target_properties"`
}

func streamGraphFanout[R any](path string, workers int, process func(*rawNode) R, collect func(R)) {
	f := throw2(os.Open(path))

	defer func() { throw(f.Close()) }()

	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	dec.UseNumber()
	seekToGraph(dec, path)

	nodes := make(chan *rawNode, workers*2)
	results := make(chan R, workers*2)

	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for n := range nodes {
				results <- process(n)
			}
		}()
	}

	done := make(chan struct{})

	go func() {
		for r := range results {
			collect(r)
		}

		close(done)
	}()

	for dec.More() {
		node := &rawNode{}
		throw(dec.Decode(node))
		nodes <- node
	}

	close(nodes)
	wg.Wait()
	close(results)
	<-done
}

func streamJSONL(path string, fn func(map[string]any)) {
	f := throw2(os.Open(path))

	defer func() { throw(f.Close()) }()

	r := bufio.NewReaderSize(f, 1<<20)

	for {
		line, err := r.ReadString('\n')

		if len(line) > 0 {
			n := map[string]any{}
			throw(json.Unmarshal([]byte(line), &n))
			fn(n)
		}

		if err == io.EOF {
			break
		}

		throw(err)
	}
}

func nodeKVP(n map[string]any) string {
	kv, _ := n["kv"].(map[string]any)
	p, _ := kv["p"].(string)

	return p
}

func seekToGraph(dec *json.Decoder, path string) {
	tok := throw2(dec.Token())

	if d, ok := tok.(json.Delim); !ok || d != '{' {
		throwFmt("dump: %s: expected top-level JSON object", path)
	}

	for dec.More() {
		key, ok := throw2(dec.Token()).(string)

		if !ok {
			throwFmt("dump: %s: expected object key", path)
		}

		if key == "graph" {
			open := throw2(dec.Token())

			if d, ok := open.(json.Delim); !ok || d != '[' {
				throwFmt("dump: %s: graph is not an array", path)
			}

			return
		}

		var skip json.RawMessage
		throw(dec.Decode(&skip))
	}

	throwFmt("dump: %s: no \"graph\" key found", path)
}
