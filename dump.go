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
	// versionedResourceRe matches a resource reference carrying an id suffix (bare
	// digits or sbr:<digits>). Normalization strips the id on both graphs so versioned
	// and bare references compare equal — these ids churn on rotation and carry no
	// build-graph meaning.
	versionedResourceRe = regexp.MustCompile(`\$\(([A-Z][A-Z0-9_]*)-(?:sbr:)?[0-9]+\)`)
	// resourceMountFoldRe folds our $(B)/resources/<NAME>/… (a real FETCH-node output)
	// back to upstream's bare $(<NAME>)/… mount so the two compare equal after the
	// FETCH node is stripped.
	resourceMountFoldRe = regexp.MustCompile(`\$\(B\)/resources/([A-Z_][A-Z0-9_]*)`)
	// cmdLiteralBasenames are bare-word command arguments that collide with a real
	// input's basename but do NOT name that file ("gnu" is the llvm-ar format selector).
	cmdLiteralBasenames = map[string]bool{"gnu": true}
)

// ldOwnScriptRels is the reference-graph AR/LD input whitelist: link-wrapper tooling
// a link node carries but never names on its command line. The normalizer has no
// access to the script tree, so it must list the full closure explicitly.
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

// objcopyOverEmitExts are the C/C++ source/header extensions making up the embedded
// resource producer's leaked compile closure on a resource-objcopy node.
var objcopyOverEmitExts = map[string]struct{}{
	".h": {}, ".hpp": {}, ".hxx": {}, ".ipp": {}, ".inc": {}, ".def": {},
	".proto": {}, ".cpp": {}, ".cc": {}, ".cxx": {}, ".c": {}, ".i": {}, ".td": {},
	".txt": {}, // COPY_FILE(TEXT) header sources (.h.txt)
}

func normPath(s string) string {
	if !strings.Contains(s, "$(") {
		return s
	}

	s = strings.ReplaceAll(s, "$(BUILD_ROOT)", "$(B)")
	s = strings.ReplaceAll(s, "$(SOURCE_ROOT)", "$(S)")

	// Fold our $(B)/resources/<NAME>/… toolchain paths back to the bare $(<NAME>)/…
	// form upstream emits.
	if strings.Contains(s, "$(B)/resources/") {
		// The trailing slash leaves the resource-global declaration value (no slash)
		// uncaught, so it keeps its versioned ref via the general fold below.
		s = strings.ReplaceAll(s, "$(B)/resources/CLANG20/", "$(CLANG)/")
		s = resourceMountFoldRe.ReplaceAllString(s, "$($1)")
	}

	// Our inline vcs.json is a real $(B)/vcs.json producer; upstream mounts it as
	// $(VCS)/vcs.json with no producer.
	s = strings.ReplaceAll(s, "$(B)/vcs.json", "$(VCS)/vcs.json")

	// Strip the id suffix from any versioned resource ref. The regex matches only refs
	// carrying an id, so it covers all resources without an allow-list.
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
	// Dedup adjacent: a node may list the same input more than once, but the canonical
	// form is a set.
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
// string for whole-path substring testing.
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
// arguments, matched on whole basenames (a trailing ':' is stripped) so foo.c is
// not matched against foo.c.o.
//
// The archiver's `-k $KEYS` value is a colon-joined list of archive keys, not input
// paths; its trailing key would leak a phantom basename, so skip the token after -k.
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
// pruning. Keep it if it is a real build artifact / known script, or if the command
// names its file. Upstream propagates the full transitive header/source set onto
// AR/LD nodes; those are never named by the command (.o/.a go through a response
// file), so they fall away while genuinely-consumed named inputs are kept.
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
// compile/source closure that upstream over-emits onto a resource-objcopy node (the
// objcopy action reads only the resource it embeds; none appear in cmd_args).
// Command-named files and $(B) artifacts are kept.
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
// survives the resource-objcopy input over-emit prune. Only genuine data-source
// leaves are meaningful: C/C++ compile noise, contrib/libs sources, and build/*.py
// tooling are discounted. The faithful graph emits exactly this kept set.
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

// canonInputs returns a node's inputs after canonical filtering. AR/LD input pruning
// applies ONLY to the upstream reference graph: it discounts inputs upstream lists
// that our generator does not model. Our graph is normalized faithfully so any
// genuine over- or under-emission surfaces as a diff.
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
		// Resource-embedding objcopy: same over-emit class as AR/LD (upstream lists
		// the producer's transitive $(S) closure though objcopy reads only the embedded
		// resource); keep only command-named inputs.
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

	// tags and host_platform are a graph-merge artifact, not intrinsic to a node's
	// build action; stripping them lets a host (tool) instance and its target twin
	// compare on real content.
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
// path. String-array fields and scalars decode directly to Go types, avoiding the
// per-element boxing of a []any decode. The fields canonContent feeds to normRec
// stay any so the canonicalized bytes match the former map[string]any decode and
// orVal's `v == nil` substitution survives (a typed-nil map in any is != nil).
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
