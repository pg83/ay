package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh/agent"
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

// cmdFetchBase64 (`ay fetch base64 <data> <out>`) writes the base64-decoded data
// straight to <out>. Used by the inline vcs.json node — it produces a file, it
// does not fetch a sandbox resource, so it is a plain build command, not a FETCH
// node.
func cmdFetchBase64(_ GlobalFlags, args []string) int {
	if len(args) != 2 {
		throwFmt("fetch: usage: ay fetch base64 <data> <out>")
	}

	data := throw2(base64.StdEncoding.DecodeString(args[0]))
	out := args[1]
	throw(os.MkdirAll(filepath.Dir(out), 0o755))
	throw(os.WriteFile(out, data, 0o644))

	return 0
}

func cmdFetch(_ GlobalFlags, args []string) int {
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
	downloadResourceArchive(sourceRoot, uri, archivePath)
	unpackResourceArchive(archivePath, out)
}

// downloadResourceArchive resolves a resource uri (sbr: / mapped MDS / plain URL)
// to a single local archive file at archivePath — the authenticated download step
// shared by fetchResource (which wipes+unpacks into a resource dir) and the
// FROM_SANDBOX path (which unpacks into the module build dir without wiping it).
func downloadResourceArchive(sourceRoot, uri, archivePath string) {
	tmp := filepath.Dir(archivePath)
	mapping := readResourceMappingMaybe(filepath.Join(sourceRoot, "build/mapping.conf.json"))

	switch {
	case strings.HasPrefix(uri, "sbr:"):
		id := strings.TrimPrefix(uri, "sbr:")
		mapped := mapping.urlForSandboxID(id)
		token := resolveSandboxToken()

		switch {
		case mapped != "":
			fetchURL(mapped, archivePath)
		case token != "":
			// Authenticated fetch, like ya: the Sandbox API and proxy both require
			// the OAuth token. fetch_from_sandbox.py is unauthenticated (401s), so we
			// only fall back to it when no token is configured.
			fetchFromSandbox(id, token, archivePath)
		default:
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
}

// cmdFetchSandbox is `ay fetch sandbox` — the authenticated, in-our-control
// replacement for upstream's fetch_from_sandbox.py that the executor runs for
// FROM_SANDBOX (pkSB) nodes. It accepts a leading --source-root plus the same
// flags the node carries, fetches the Sandbox resource with the OAuth token, and
// lays its outputs into the module build dir (cwd) per --untar-to / --copy-to-dir
// / RENAME / EXECUTABLE. fetch_from_sandbox.py's own metadata query is
// unauthenticated (401s), which is the whole reason we bypass it.
func cmdFetchSandbox(_ GlobalFlags, args []string) int {
	var sourceRoot, id, copyToDir, untarTo string
	executable := false

	var renames, outs []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--source-root":
			i++
			sourceRoot = args[i]
		case "--resource-id":
			i++
			id = args[i]
		case "--untar-to":
			i++
			untarTo = args[i]
		case "--copy-to-dir":
			i++
			copyToDir = args[i]
		case "--rename":
			// fetch_from.py's --rename is action='append' (one FILE per flag),
			// pairing positionally with the outputs after `--`; it is NOT a two-token
			// old/new pair. The special FILE name RESOURCE denotes the fetched
			// resource itself.
			i++
			renames = append(renames, args[i])
		case "--executable":
			executable = true
		case "--resource-file":
			i++ // we fetch ourselves; ignore the (absent) pre-fetched file path
		case "--ya-start-command-file", "--ya-end-command-file":
			// command-file markers — irrelevant once args are expanded
		case "--":
			// Everything after `--` is OUT/OUT_NOAUTO, except a trailing
			// command-file marker that rode along in the same arg span.
			for _, o := range args[i+1:] {
				if o == "--ya-start-command-file" || o == "--ya-end-command-file" {
					continue
				}

				outs = append(outs, o)
			}

			i = len(args)
		}
	}

	if id == "" {
		throwFmt("fetch sandbox: missing --resource-id")
	}

	tmp := throw2(os.MkdirTemp("", "ay-sb-*"))

	defer os.RemoveAll(tmp)

	archivePath := filepath.Join(tmp, "resource")
	downloadResourceArchive(sourceRoot, "sbr:"+id, archivePath)

	placeSandboxResource(archivePath, copyToDir, untarTo, renames, outs, executable)

	return 0
}

// placeSandboxResource lays a fetched Sandbox resource into the node's build dir
// (cwd), mirroring build/scripts/fetch_from.py's process(): untar the archive when
// --untar-to is set, then resolve each RENAME against its positional output —
// RESOURCE means the fetched file itself (copied onto the output), any other name
// is an extracted member moved onto the output — and, for a bare --copy-to-dir
// with no RENAME, copy the single fetched file to the declared output name.
// Output paths are relative to cwd, as upstream emits them.
func placeSandboxResource(fetched, copyToDir, untarTo string, renames, outs []string, executable bool) {
	if len(renames) > len(outs) {
		throwFmt("fetch sandbox: %d renames exceed %d outputs", len(renames), len(outs))
	}

	if untarTo != "" {
		unpackResourceArchive(fetched, untarTo)
	}

	for idx, src := range renames {
		dst := outs[idx]
		throw(os.MkdirAll(filepath.Dir(dst), 0o755))

		if src == "RESOURCE" {
			throw(copyFile(fetched, dst))

			continue
		}

		s := src
		if untarTo != "" {
			s = filepath.Join(untarTo, src)
		}

		throw(os.Rename(s, dst))
	}

	if copyToDir != "" && len(renames) == 0 {
		throw(os.MkdirAll(copyToDir, 0o755))

		dst := filepath.Join(copyToDir, "resource")
		if len(outs) == 1 {
			dst = filepath.Join(copyToDir, outs[0])
		}

		throw(copyFile(fetched, dst))
	}

	if executable {
		for _, o := range outs {
			throw(os.Chmod(o, 0o755))
		}
	}
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
	httpGetToFile(raw, "", out)
}

// httpGetToFile downloads raw to out, sending an `Authorization: OAuth <token>` header
// when token is non-empty (Sandbox proxy/MDS downloads require it; public mirrors do not).
func httpGetToFile(raw, token, out string) {
	if raw == "" {
		throwFmt("fetch: empty URL")
	}

	req := throw2(http.NewRequest(http.MethodGet, raw, nil))

	if token != "" {
		req.Header.Set("Authorization", "OAuth "+token)
	}

	resp := throw2(http.DefaultClient.Do(req))

	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		throwFmt("fetch: %s returned %s", raw, resp.Status)
	}

	f := throw2(os.Create(out))

	defer f.Close()

	throw2(io.Copy(f, resp.Body))
}

const (
	sandboxAPIBase      = "https://sandbox.yandex-team.ru/api/v1.0"
	sandboxOriginSuffix = "?origin=fetch-from-sandbox"
	mdsGetSandboxPrefix = "http://storage-int.mds.yandex.net/get-sandbox/"
)

// OAuth client used to exchange an SSH key for a token (devtools/ya/yalibrary/oauth).
const (
	oauthClientID     = "f4d36b7671004ed9850148fa645acac6"
	oauthClientSecret = "da475ea72e58427ab5c8a31e17ef2347"
	oauthTokenURL     = "https://oauth.yandex-team.ru/token"
)

// argsNeedSandboxToken reports whether a fetch command targets a Sandbox resource (an
// sbr: URI), the only fetch kind that requires an OAuth token.
func argsNeedSandboxToken(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "sbr:") {
			return true
		}
	}

	return false
}

// resolveSandboxToken returns the Sandbox OAuth token the way ya does: $YA_TOKEN, then
// the ~/.ya_token file, then an SSH-agent key exchanged for a token. Empty means no
// token could be obtained (callers fall back to the unauthenticated fetch script).
func resolveSandboxToken() string {
	if t := strings.TrimSpace(os.Getenv("YA_TOKEN")); t != "" {
		return t
	}

	if home, err := os.UserHomeDir(); err == nil {
		if b, err := os.ReadFile(filepath.Join(home, ".ya_token")); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				return t
			}
		}
	}

	return tokenFromSSHAgent(oauthLogin())
}

// oauthLogin is the Staff login used for the SSH-key exchange: $YA_USER, else the current
// OS user (matching ya's getpass.getuser()).
func oauthLogin() string {
	if u := strings.TrimSpace(os.Getenv("YA_USER")); u != "" {
		return u
	}

	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}

	return os.Getenv("USER")
}

// tokenFromSSHAgent reimplements ya's library.python.oauth.get_token SSH path: sign
// "<ts><client_id><login>" with each SSH-agent key and POST grant_type=ssh_key to the
// OAuth service; the first key the server accepts yields the token. Returns "" if no
// agent is reachable or no key is accepted.
func tokenFromSSHAgent(login string) string {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" || login == "" {
		return ""
	}

	conn, err := net.Dial("unix", sock)
	if err != nil {
		return ""
	}

	defer conn.Close()

	ag := agent.NewClient(conn)

	keys, err := ag.List()
	if err != nil {
		return ""
	}

	ts := time.Now().Unix()
	data := []byte(strconv.FormatInt(ts, 10) + oauthClientID + login)

	for _, key := range keys {
		sig, err := ag.Sign(key, data)
		if err != nil {
			continue
		}

		form := url.Values{
			"grant_type":    {"ssh_key"},
			"client_id":     {oauthClientID},
			"client_secret": {oauthClientSecret},
			"login":         {login},
			"ts":            {strconv.FormatInt(ts, 10)},
			// The signature is the second SSH-string of the agent's reply; x/crypto
			// hands it back already split out as sig.Blob. base64url, no padding.
			"ssh_sign": {base64.RawURLEncoding.EncodeToString(sig.Blob)},
		}

		// A certificate key isn't pre-registered on Staff, so the server needs the
		// cert itself to verify (ya sends public_cert only for cert keys).
		if strings.Contains(key.Format, "cert") {
			form.Set("public_cert", base64.RawURLEncoding.EncodeToString(key.Blob))
		}

		if tok := postOAuthToken(form); tok != "" {
			return tok
		}
	}

	return ""
}

// postOAuthToken posts the token request and returns access_token, or "" on any failure.
func postOAuthToken(form url.Values) string {
	resp, err := http.PostForm(oauthTokenURL, form)
	if err != nil {
		return ""
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var out struct {
		AccessToken string `json:"access_token"`
	}

	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return ""
	}

	return out.AccessToken
}

// sandboxResource is the subset of the Sandbox resource API response we consume.
type sandboxResource struct {
	State     string `json:"state"`
	Multifile bool   `json:"multifile"`
	HTTP      struct {
		Proxy string `json:"proxy"`
	} `json:"http"`
	Attributes struct {
		MDS string `json:"mds"`
	} `json:"attributes"`
}

// fetchFromSandbox downloads Sandbox resource id to archivePath using the OAuth token,
// mirroring ya's authenticated fetch (build/scripts/fetch_from_sandbox.py is
// unauthenticated): query the resource API, then download the tokenless MDS mirror when
// present, else the proxy link (which also requires the token).
func fetchFromSandbox(id, token, archivePath string) {
	info := querySandboxResource(id, token)

	if info.State != "READY" {
		throwFmt("fetch: sandbox resource %s is not READY (state=%s)", id, info.State)
	}

	// Download sources tried in order, like ya (skynet/proxy/storage with fallback):
	// the tokenless MDS mirror first when present, then the authenticated proxy. An old
	// resource's MDS object can be 410 Gone while the proxy still serves it, so a single
	// source is not enough.
	type source struct {
		url  string
		auth bool
	}

	var sources []source

	if mds := info.Attributes.MDS; mds != "" {
		sources = append(sources, source{url: mdsGetSandboxPrefix + mds})
	}

	proxy := info.HTTP.Proxy + sandboxOriginSuffix
	if info.Multifile {
		proxy += "&stream=tgz"
	}

	sources = append(sources, source{url: proxy, auth: true})

	var last *Exception

	for _, s := range sources {
		tok := ""
		if s.auth {
			tok = token
		}

		if last = try(func() { httpGetToFile(s.url, tok, archivePath) }); last == nil {
			return
		}
	}

	last.throw()
}

func querySandboxResource(id, token string) sandboxResource {
	req := throw2(http.NewRequest(http.MethodGet, sandboxAPIBase+"/resource/"+id, nil))
	req.Header.Set("Authorization", "OAuth "+token)

	resp := throw2(http.DefaultClient.Do(req))

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		throwFmt("fetch: sandbox resource %s API returned %s", id, resp.Status)
	}

	var info sandboxResource

	throw(json.NewDecoder(resp.Body).Decode(&info))

	return info
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
