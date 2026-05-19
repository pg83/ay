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
	Ref     NodeRef
}

type resourceFetchPlan struct {
	items []resourceFetch
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
			Output:  Build("resources/" + r.Pattern),
		})
	}

	sort.Slice(plan.items, func(i, j int) bool {
		return plan.items[i].Pattern < plan.items[j].Pattern
	})

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

func (p *resourceFetchPlan) emitAll(host *Platform, emit Emitter) {
	if p == nil {
		return
	}

	for i := range p.items {
		ref := emit.Emit(fetchNode(host, p.items[i]))
		p.items[i].Ref = ref
	}
}

func fetchNode(host *Platform, item resourceFetch) *Node {
	return &Node{
		Cmds: []Cmd{{
			CmdArgs: []string{
				currentYatoolPath(),
				"fetch",
				"$(B)",
				"$(S)",
				item.URI,
				item.Output.String(),
			},
			Env: map[string]string{},
		}},
		Env:          map[string]string{},
		Inputs:       fetchScriptInputs(),
		KV:           map[string]string{"p": "FETCH", "pc": "yellow", "show_out": "yes"},
		Outputs:      []VFS{item.Output},
		Platform:     string(host.Target),
		Requirements: map[string]interface{}{"cpu": float64(1), "network": "full", "ram": float64(32)},
		Sandboxing:   true,
		Tags:         append([]string(nil), host.Tags...),
		TargetProperties: map[string]string{
			"module_dir": "build/resources",
		},
	}
}

func currentYatoolPath() string {
	path := Throw2(os.Executable())

	return path
}

func fetchScriptInputs() []VFS {
	inputs := []VFS{
		Source("build/mapping.conf.json"),
		Source("build/scripts/error.py"),
		Source("build/scripts/fetch_from.py"),
		Source("build/scripts/fetch_from_mds.py"),
		Source("build/scripts/fetch_from_sandbox.py"),
		Source("build/scripts/process_command_files.py"),
		Source("build/scripts/retry.py"),
	}
	SortVFS(inputs)

	return inputs
}

type resourceAwareEmitter struct {
	inner Emitter
	plan  *resourceFetchPlan
}

func newResourceAwareEmitter(inner Emitter, plan *resourceFetchPlan) Emitter {
	if plan == nil || len(plan.items) == 0 {
		return inner
	}

	return &resourceAwareEmitter{inner: inner, plan: plan}
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

func (e *resourceAwareEmitter) attachResourceDeps(n *Node) {
	for _, item := range e.plan.items {
		if !nodeUsesResourcePattern(n, item.Pattern) {
			continue
		}

		n.DepRefs = append(n.DepRefs, item.Ref)
	}
}

func nodeUsesResourcePattern(n *Node, pattern string) bool {
	token := "$(" + pattern + ")"

	for _, cmd := range n.Cmds {
		if strings.Contains(cmd.Cwd, token) || strings.Contains(cmd.Stdout, token) {
			return true
		}

		for _, arg := range cmd.CmdArgs {
			if strings.Contains(arg, token) {
				return true
			}
		}

		for _, v := range cmd.Env {
			if strings.Contains(v, token) {
				return true
			}
		}
	}

	for _, v := range n.Env {
		if strings.Contains(v, token) {
			return true
		}
	}

	return false
}

func (p *resourceFetchPlan) mountMap() map[string]string {
	out := map[string]string{}

	if p == nil {
		return out
	}

	for _, item := range p.items {
		out[item.Pattern] = item.Output.Rel
	}

	return out
}

func cmdFetch(args []string) int {
	if len(args) != 3 && len(args) != 4 {
		ThrowFmt("fetch: usage: yatool fetch <build-root> <source-root> <uri> [output-dir]")
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

func fetchResource(sourceRoot, uri, out string) {
	Throw(os.RemoveAll(out))
	Throw(os.MkdirAll(out, 0o755))

	tmp := Throw2(os.MkdirTemp("", "yatool-fetch-*"))
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
