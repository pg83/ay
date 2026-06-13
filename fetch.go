package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ResourceMappingConf struct {
	Extensions map[string]string `json:"extensions"`
	Resources  map[string]string `json:"resources"`
}

func currentYatoolPath() string {
	path := throw2(os.Executable())

	return path
}

// fetchScriptInputs is the FETCH node's $(S) inputs: the resource-mapping config
// plus the two fetch scripts `ay fetch` actually runs (fetch_from_sandbox /
// fetch_from_mds), each expanded to its import closure via the table — which pulls
// in error.py, fetch_from.py, retry.py and process_command_files.py.
func fetchScriptInputs(scripts ScriptDeps) []VFS {
	out := []VFS{buildMappingConfJson}
	out = append(out, scripts[buildScriptsFetchFromSandboxPy]...)
	out = append(out, scripts[buildScriptsFetchFromMdsPy]...)

	return out
}

func cmdFetch(args []string) int {
	// `ay fetch base64 <data> <out>` writes the base64-decoded data straight to
	// <out>. Used by the inline vcs.json node — it produces a file, it does not
	// fetch a sandbox resource, so it is a plain build command, not a FETCH node.
	if len(args) >= 1 && args[0] == "base64" {
		if len(args) != 3 {
			throwFmt("fetch: usage: ay fetch base64 <data> <out>")
		}

		data := throw2(base64.StdEncoding.DecodeString(args[1]))
		out := args[2]
		throw(os.MkdirAll(filepath.Dir(out), 0o755))
		throw(os.WriteFile(out, data, 0o644))

		return 0
	}

	if len(args) != 3 && len(args) != 4 {
		throwFmt("fetch: usage: ay fetch <build-root> <source-root> <uri> [output-dir]")
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
	throw(forceRemoveAll(out))
	throw(os.MkdirAll(out, 0o755))

	tmp := throw2(os.MkdirTemp("", "ay-fetch-*"))

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

func readResourceMappingMaybe(path string) ResourceMappingConf {
	var out ResourceMappingConf
	raw, err := os.ReadFile(path)

	if err != nil {
		return out
	}

	throw(json.Unmarshal(raw, &out))

	return out
}

func (m ResourceMappingConf) urlForSandboxID(id string) string {
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

	throw(cmd.Run())
}

func fetchURL(raw, out string) {
	if raw == "" {
		throwFmt("fetch: empty URL")
	}

	resp := throw2(http.Get(raw))

	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		throwFmt("fetch: %s returned %s", raw, resp.Status)
	}

	f := throw2(os.Create(out))

	defer f.Close()

	throw2(io.Copy(f, resp.Body))
}

func unpackResourceArchive(archivePath, out string) {
	throw(os.MkdirAll(out, 0o755))

	cmd := exec.Command("tar", "-xf", archivePath, "-C", out)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err == nil {
		return
	}

	throw(os.MkdirAll(out, 0o755))

	zipCmd := exec.Command("unzip", "-q", archivePath, "-d", out)
	zipCmd.Stdout = os.Stdout
	zipCmd.Stderr = os.Stderr

	if err := zipCmd.Run(); err == nil {
		return
	}

	throw(os.MkdirAll(out, 0o755))

	dst := filepath.Join(out, "resource")
	throw(copyFile(archivePath, dst))
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
