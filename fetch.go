package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type resourceFetch struct {
	Pattern string
	URI     string
	Output  VFS
}

type resourceFetchPlan struct {
	items     []resourceFetch
	byPattern map[string]int // bare pattern -> index into items
}

type resourceMappingConf struct {
	Extensions map[string]string `json:"extensions"`
	Resources  map[string]string `json:"resources"`
}

func newResourceFetchPlan(_ string, conf *graphConf, host *Platform) *resourceFetchPlan {
	plan := &resourceFetchPlan{}

	if conf == nil || len(conf.Resources) == 0 {
		return plan
	}

	for _, r := range conf.Resources {
		if r.Pattern == "" || len(r.Resources) == 0 {
			continue
		}

		uri := selectHostResourceURI(r.Resources, host)

		if uri == "" {
			continue
		}

		plan.items = append(plan.items, resourceFetch{
			Pattern: r.Pattern,
			URI:     uri,
			Output:  Intern("$(B)/resources/" + r.Pattern),
		})
	}

	sort.Slice(plan.items, func(i, j int) bool {
		return plan.items[i].Pattern < plan.items[j].Pattern
	})

	plan.byPattern = make(map[string]int, len(plan.items))

	for i, it := range plan.items {
		plan.byPattern[it.Pattern] = i
	}

	return plan
}

func selectHostResourceURI(resources []graphConfResourceURI, host *Platform) string {
	candidates := hostResourcePlatformCandidates(host)

	for _, cand := range candidates {
		for _, r := range resources {
			if r.Platform == cand {
				return r.Resource
			}
		}
	}

	return ""
}

func hostResourcePlatformCandidates(host *Platform) []string {
	if host == nil {
		return []string{"linux", "LINUX"}
	}

	base := string(host.OS)
	withISA := base + "-" + string(host.ISA)

	if host.ISA == ISAX8664 {
		return []string{
			base,
			strings.ToUpper(base),
			withISA,
			strings.ToUpper(withISA),
		}
	}

	return []string{
		withISA,
		strings.ToUpper(withISA),
		base,
		strings.ToUpper(base),
	}
}

func fetchNode(host *Platform, item resourceFetch, scripts scriptDeps) *Node {
	return bindNodePlatform(&Node{
		Cmds: []Cmd{{
			CmdArgs: []STR{
				internStr(currentYatoolPath()),
				argFetch.str(),
				argB.str(),
				argS.str(),
				internStr(item.URI),
				(item.Output).str(),
			},
			Env: nil,
		}},
		Env:              nil,
		Inputs:           fetchScriptInputs(scripts),
		KV:               KV{P: pkFETCH, PC: pcYellow, ShowOut: "yes"},
		Outputs:          []VFS{item.Output},
		Requirements:     Requirements{CPU: float64(1), Network: "full", RAM: float64(32)},
		Sandboxing:       true,
		Tags:             host.Tags, // read-only; Platform.Tags is immutable during emit
		TargetProperties: TargetProperties{ModuleDir: "build/resources"},
	}, host)
}

func currentYatoolPath() string {
	path := Throw2(os.Executable())

	return path
}

// fetchScriptInputs is the FETCH node's $(S) inputs: the resource-mapping config
// plus the two fetch scripts `ay fetch` actually runs (fetch_from_sandbox /
// fetch_from_mds), each expanded to its import closure via the table — which pulls
// in error.py, fetch_from.py, retry.py and process_command_files.py.
func fetchScriptInputs(scripts scriptDeps) []VFS {
	out := []VFS{buildMappingConfJson}
	out = append(out, scripts[buildScriptsFetchFromSandboxPy]...)
	out = append(out, scripts[buildScriptsFetchFromMdsPy]...)
	return out
}

type resourceAwareEmitter struct {
	inner   Emitter
	plan    *resourceFetchPlan
	host    *Platform
	scripts scriptDeps
	refs    []NodeRef
	seen    BitSet
}

func newResourceAwareEmitter(host *Platform, inner Emitter, plan *resourceFetchPlan, scripts scriptDeps) Emitter {
	if plan == nil || len(plan.items) == 0 {
		return inner
	}

	return &resourceAwareEmitter{
		inner:   inner,
		plan:    plan,
		host:    host,
		scripts: scripts,
		refs:    make([]NodeRef, len(plan.items)),
	}
}

func resourceGraphEmitter(host *Platform, inner Emitter, plan *resourceFetchPlan, materializeFetchNodes bool, scripts scriptDeps) Emitter {
	if !materializeFetchNodes || plan == nil || len(plan.items) == 0 {
		return inner
	}

	if plan.byPattern == nil {
		plan.byPattern = make(map[string]int, len(plan.items))

		for i, it := range plan.items {
			plan.byPattern[it.Pattern] = i
		}
	}

	return newResourceAwareEmitter(host, inner, plan, scripts)
}

func (e *resourceAwareEmitter) Emit(n *Node) NodeRef {
	e.attachResourceDeps(n)

	return e.inner.Emit(n)
}

func (e *resourceAwareEmitter) Result(r NodeRef) {
	e.inner.Result(r)
}

func (e *resourceAwareEmitter) OnReady(r NodeRef) <-chan struct{} {
	return e.inner.OnReady(r)
}

// attachResourceDeps turns each resource pattern the node declared (at the
// site that spliced $(PATTERN) into its command) into a dependency on that
// resource's fetch node — emitting the fetch node once per pattern. No command
// scanning: usesResources is the authoritative, builder-declared set.
func (e *resourceAwareEmitter) attachResourceDeps(n *Node) {
	for _, pat := range n.usesResources {
		i, ok := e.plan.byPattern[pat]

		if !ok {
			continue // resource not in this run's fetch plan (e.g. unselected host)
		}

		if !e.seen.has(uint32(i)) {
			e.refs[i] = e.inner.Emit(fetchNode(e.host, e.plan.items[i], e.scripts))
			e.seen.add(uint32(i))
		}

		n.DepRefs = append(n.DepRefs, e.refs[i])
	}
}

func (p *resourceFetchPlan) mountMap() map[string]string {
	out := map[string]string{}

	if p == nil {
		return out
	}

	for _, item := range p.items {
		out[item.Pattern] = item.Output.Rel()
	}

	return out
}

func cmdFetch(args []string) int {
	if len(args) != 3 && len(args) != 4 {
		ThrowFmt("fetch: usage: ay fetch <build-root> <source-root> <uri> [output-dir]")
	}

	buildRoot := args[0]
	sourceRoot := args[1]
	uri := args[2]
	out := filepath.Join(buildRoot, "resources", resourceOutputName(uri))

	if len(args) == 4 {
		out = args[3]
	}

	fetchResource(sourceRoot, uri, out)

	return 0
}

func resourceOutputName(uri string) string {
	name := strings.TrimPrefix(uri, "sbr:")
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, ":", "_")

	return name
}

// forceRemoveAll removes path even when it contains read-only directories.
// Extracted sandbox resource trees (e.g. CLANG) are unpacked with their archived
// permissions, which are frequently write-less on directories; plain
// os.RemoveAll then cannot unlink the entries inside them ("permission denied").
// On the first failure it walks the tree making every directory writable and
// retries once.
func forceRemoveAll(path string) error {
	if err := os.RemoveAll(path); err == nil {
		return nil
	}

	_ = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = os.Chmod(p, 0o755)
		}

		return nil
	})
	return os.RemoveAll(path)
}

func fetchResource(sourceRoot, uri, out string) {
	Throw(forceRemoveAll(out))
	Throw(os.MkdirAll(out, 0o755))

	tmp := Throw2(os.MkdirTemp("", "ay-fetch-*"))
	defer os.RemoveAll(tmp)

	archivePath := filepath.Join(tmp, "resource")
	mapping := readResourceMappingMaybe(filepath.Join(sourceRoot, "build/mapping.conf.json"))

	switch {
	case strings.HasPrefix(uri, "sbr:"):
		id := strings.TrimPrefix(uri, "sbr:")
		mapped := mapping.urlForSandboxID(id)

		if mapped != "" {
			fetchURL(mapped, archivePath)
		} else {
			runPythonScript(tmp, filepath.Join(sourceRoot, "build/scripts/fetch_from_sandbox.py"),
				"--resource-id", id,
				"--copy-to", archivePath,
			)
		}
	case mdsKeyFromMappedResource(uri) != "":
		runPythonScript(tmp, filepath.Join(sourceRoot, "build/scripts/fetch_from_mds.py"),
			"--key", mdsKeyFromMappedResource(uri),
			"--copy-to", archivePath,
		)
	default:
		fetchURL(uri, archivePath)
	}

	unpackResourceArchive(archivePath, out)
}

func readResourceMappingMaybe(path string) resourceMappingConf {
	var out resourceMappingConf
	raw, err := os.ReadFile(path)

	if err != nil {
		return out
	}

	Throw(json.Unmarshal(raw, &out))

	return out
}

func (m resourceMappingConf) urlForSandboxID(id string) string {
	tpl := m.Resources[id]

	if tpl == "" {
		return ""
	}

	for k, v := range m.Extensions {
		tpl = strings.ReplaceAll(tpl, "{"+k+"}", v)
	}

	return tpl
}

func mdsKeyFromMappedResource(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
		key := strings.TrimPrefix(u.Path, "/")

		if strings.Count(key, "/") == 2 {
			return key
		}

		return ""
	}

	if strings.Count(raw, "/") == 2 {
		return raw
	}

	return ""
}

func runPythonScript(cwd, script string, args ...string) {
	cmdArgs := append([]string{script}, args...)
	cmd := exec.Command("python3", cmdArgs...)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	Throw(cmd.Run())
}

func fetchURL(raw, out string) {
	if raw == "" {
		ThrowFmt("fetch: empty URL")
	}

	resp := Throw2(http.Get(raw))
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		ThrowFmt("fetch: %s returned %s", raw, resp.Status)
	}

	f := Throw2(os.Create(out))
	defer f.Close()

	Throw2(io.Copy(f, resp.Body))
}

func unpackResourceArchive(archivePath, out string) {
	Throw(os.MkdirAll(out, 0o755))

	cmd := exec.Command("tar", "-xf", archivePath, "-C", out)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err == nil {
		return
	}

	Throw(os.MkdirAll(out, 0o755))

	zipCmd := exec.Command("unzip", "-q", archivePath, "-d", out)
	zipCmd.Stdout = os.Stdout
	zipCmd.Stderr = os.Stderr

	if err := zipCmd.Run(); err == nil {
		return
	}

	Throw(os.MkdirAll(out, 0o755))

	dst := filepath.Join(out, "resource")
	Throw(copyFile(archivePath, dst))
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)

	if err != nil {
		return err
	}

	defer in.Close()

	out, err := os.Create(dst)

	if err != nil {
		return err
	}

	_, err = io.Copy(out, in)

	if err != nil {
		_ = out.Close()
		return err
	}

	return out.Close()
}
