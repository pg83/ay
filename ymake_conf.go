package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var ymakeConfEnvKeys = []string{
	"PATH", "PATHEXT", "SYSTEMROOT", "USERPROFILE", "VCINSTALLDIR",
	"VCTOOLSINSTALLDIR", "WINDOWSSDKDIR", "WINDOWSSDKVERSION",
}

const (
	ymakeConfScript    = "build/ymake_conf.py"
	yaConfJSON         = "build/ya.conf.json"
	yaConfJSONOpenSrc  = "devtools/ya/opensource/ya.conf.json"
	localYmakeConf     = "local.ymake"
	toolchainCxxTool   = "c++"
	toolchainArchDflt  = "x86_64"
	toolchainNameDflt  = "default"
	confBuildTypeDist  = "dist-"
	confVerbosityQuiet = "no"
)

type Toolchain struct {
	Name      string
	MatchRoot string
	ToolVar   string
	Params    map[string]any
}

type YmakeConfRun struct {
	cmd    *exec.Cmd
	script string
	stdout bytes.Buffer
	stderr bytes.Buffer
}

type YaConfToolchains struct {
	order   []string
	entries map[string]map[string]any
	bottles map[string]any
}

func toolchainPlatformName(os OS, isa ISA) string {
	return strings.ToUpper(toolchainNameDflt + "-" + string(os) + "-" + string(isa))
}

func readYaConfToolchains(fs FS) *YaConfToolchains {
	rel := yaConfJSON

	if !fs.isFile(srcRootRel, rel) {
		rel = yaConfJSONOpenSrc
	}

	if !fs.isFile(srcRootRel, rel) {
		throwFmt("ymake conf: no tool chain description, neither %s nor %s", yaConfJSON, yaConfJSONOpenSrc)
	}

	dec := json.NewDecoder(bytes.NewReader(concatChunks(fs.read(rel))))
	out := &YaConfToolchains{entries: map[string]map[string]any{}}

	expectJSONDelim(dec, '{')

	for dec.More() {
		switch key := jsonKey(dec); key {
		case "toolchain":
			expectJSONDelim(dec, '{')

			for dec.More() {
				name := jsonKey(dec)

				var entry map[string]any

				throw(dec.Decode(&entry))
				out.order = append(out.order, name)
				out.entries[name] = entry
			}

			expectJSONDelim(dec, '}')
		case "bottles":
			throw(dec.Decode(&out.bottles))
		default:
			var skip json.RawMessage

			throw(dec.Decode(&skip))
		}
	}

	return out
}

func expectJSONDelim(dec *json.Decoder, want json.Delim) {
	if tok := throw2(dec.Token()); tok != want {
		throwFmt("ya.conf.json: want %q, got %v", want, tok)
	}
}

func jsonKey(dec *json.Decoder) string {
	key, ok := throw2(dec.Token()).(string)

	if !ok {
		throwFmt("ya.conf.json: object key is not a string")
	}

	return key
}

func toolchainPlatform(name string, spec any, fallback map[string]any) map[string]any {
	m, _ := spec.(map[string]any)

	if m == nil {
		if fallback == nil {
			throwFmt("ya.conf.json: tool chain %s lacks a host platform", name)
		}

		return maps.Clone(fallback)
	}

	arch, _ := m["arch"].(string)

	if arch == "" {
		arch = toolchainArchDflt
	}

	return map[string]any{"os": m["os"], "arch": arch, "toolchain": name, "visible_name": name}
}

func stringizeToolchainPlatform(p map[string]any, sep string) string {
	os, _ := p["os"].(string)
	arch, _ := p["arch"].(string)
	tc, _ := p["toolchain"].(string)

	return strings.ToUpper(tc + sep + os + sep + arch)
}

func resolveCxxToolchain(fs FS, cfg *YaConfToolchains, host, target string) *Toolchain {
	for _, key := range cfg.order {
		descr := cfg.entries[key]
		tools, _ := descr["tools"].(map[string]any)
		tool, ok := tools[toolchainCxxTool].(map[string]any)

		if !ok {
			continue
		}

		name := key

		if n, ok := descr["name"].(string); ok {
			name = n
		}

		platforms, _ := descr["platforms"].([]any)

		for _, raw := range platforms {
			spec, _ := raw.(map[string]any)
			h := toolchainPlatform(name, spec["host"], nil)
			t := toolchainPlatform(name, spec["target"], h)
			candidates := [][2]map[string]any{{h, t}}

			if isDefault, _ := spec["default"].(bool); isDefault {
				hd, td := maps.Clone(h), maps.Clone(t)

				hd["toolchain"] = toolchainNameDflt
				td["toolchain"] = toolchainNameDflt
				candidates = append(candidates, [2]map[string]any{hd, td})
			}

			for _, c := range candidates {
				if stringizeToolchainPlatform(c[0], "-") == host && stringizeToolchainPlatform(c[1], "-") == target {
					return newToolchain(fs, cfg, key, descr, tool, c[0], c[1])
				}
			}
		}
	}

	throwFmt("ymake conf: unsupported platforms host=%s target=%s for tool %s", host, target, toolchainCxxTool)

	return nil
}

func newToolchain(fs FS, cfg *YaConfToolchains, key string, descr, tool, host, target map[string]any) *Toolchain {
	params, _ := descr["params"].(map[string]any)
	env, _ := descr["env"].(map[string]any)
	bottle, _ := tool["bottle"].(string)

	var formula any

	if bottle != "" {
		if b, ok := cfg.bottles[bottle].(map[string]any); ok {
			formula = b["formula"]
		}
	}

	info := map[string]any{
		"platform":        map[string]any{"host": host, "target": target},
		"env":             orEmptyMap(env),
		"params":          orEmptyMap(params),
		"formula":         formula,
		"name":            key,
		"bottle_name":     nilIfEmpty(bottle),
		"executable_path": tool["executable"],
	}

	tc := &Toolchain{Name: key, Params: info}
	root, _ := params["match_root"].(string)

	if root == "" {
		return tc
	}

	if useBundle, _ := params["use_bundle"].(bool); useBundle && formula != nil {
		tc.ToolVar = mappingVarName(root, toolchainFormulaValue(fs, formula))
	} else {
		tc.ToolVar = stringizeToolchainPlatform(target, "_")
	}

	tc.MatchRoot = root

	info["tool_var"] = tc.ToolVar
	info["env"] = substToolVar(info["env"], root, tc.ToolVar)
	info["params"] = substToolVar(info["params"], root, tc.ToolVar)

	return tc
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}

	return m
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}

func toolchainFormulaValue(fs FS, formula any) map[string]any {
	if m, ok := formula.(map[string]any); ok {
		return m
	}

	rel, _ := formula.(string)

	if !fs.isFile(srcRootRel, rel) {
		throwFmt("ymake conf: tool chain formula %s is missing", rel)
	}

	var m map[string]any

	throw(json.Unmarshal(concatChunks(fs.read(rel)), &m))

	return m
}

func substToolVar(x any, root, toolVar string) any {
	switch v := x.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))

		for k, e := range v {
			out[substToolVar(k, root, toolVar).(string)] = substToolVar(e, root, toolVar)
		}

		return out
	case []any:
		out := make([]any, len(v))

		for i, e := range v {
			out[i] = substToolVar(e, root, toolVar)
		}

		return out
	case string:
		if v == root {
			return toolVar
		}

		return strings.ReplaceAll(v, "$("+root+")", "$("+toolVar+")")
	}

	return x
}

func mappingVarName(base string, formula map[string]any) string {
	byPlatform, ok := formula["by_platform"].(map[string]any)

	if !ok || len(byPlatform) == 0 {
		throwFmt("ymake conf: tool chain formula has no 'by_platform' mapping")
	}

	type resourceDesc struct {
		uri   string
		strip uint64
	}

	canon := make(map[string]resourceDesc, len(byPlatform))

	for platform, raw := range byPlatform {
		dest, _ := raw.(map[string]any)
		uri, _ := dest["uri"].(string)
		strip, _ := dest["strip_prefix"].(float64)

		canon[canonizeResourcePlatform(platform)] = resourceDesc{uri: uri, strip: uint64(strip)}
	}

	keys := make([]string, 0, len(canon))

	for k := range canon {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var acc uint64

	for _, k := range keys {
		desc := canon[k]

		acc = utilCombineHashes(acc, utilCombineHashes(cityHash64([]byte(k)), cityHash64([]byte(desc.uri))^desc.strip))
	}

	return base + "-" + strconv.FormatUint(uint64(uint32(acc)), 10)
}

func canonizeResourcePlatform(platform string) string {
	p := strings.ToLower(platform)

	switch {
	case p == "any":
		return p
	case strings.HasPrefix(p, "linux"), strings.HasPrefix(p, "darwin"):
		os := "linux"

		if strings.HasPrefix(p, "darwin") {
			os = "darwin"
		}

		for _, arch := range []string{"aarch64", "ppc64le", "arm64"} {
			if strings.Contains(p, arch) {
				return os + "-" + arch
			}
		}

		return os
	case strings.HasPrefix(p, "win"):
		return "win32"
	}

	throwFmt("ymake conf: unsupported resource platform %q", platform)

	return ""
}

func readLocalYmake(fs FS) [][2]string {
	if !fs.isFile(srcRootRel, localYmakeConf) {
		return nil
	}

	var kv [][2]string

	for _, line := range strings.Split(string(concatChunks(fs.read(localYmakeConf))), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		k, v, ok := strings.Cut(line, "=")

		if !ok || strings.TrimSpace(k) == "SANITIZER_TYPE" {
			continue
		}

		kv = append(kv, [2]string{strings.TrimSpace(k), strings.TrimSpace(v)})
	}

	slices.SortStableFunc(kv, func(a, b [2]string) int {
		return strings.Compare(a[0], b[0])
	})

	return kv
}

func extendConfBuildType(buildType string) string {
	if strings.HasPrefix(buildType, confBuildTypeDist) {
		return buildType
	}

	return confBuildTypeDist + buildType
}

func startYmakeConf(fs FS, srcRoot, buildType string, localDistbuild bool, localFlags [][2]string, flags map[string]string, tc *Toolchain) *YmakeConfRun {
	if !fs.isFile(srcRootRel, ymakeConfScript) {
		throwFmt("ymake conf: %s is missing in %s", ymakeConfScript, srcRoot)
	}

	script := filepath.Join(srcRoot, ymakeConfScript)
	params := throw2(json.Marshal(tc.Params))

	args := []string{
		"-B", script, srcRoot, extendConfBuildType(buildType), confVerbosityQuiet,
		"--toolchain-params", base64.StdEncoding.EncodeToString(params),
	}

	if localDistbuild {
		args = append(args, "-l")
	}

	for _, kv := range localFlags {
		args = append(args, "-D", kv[0]+"="+kv[1])
	}

	keys := make([]string, 0, len(flags))

	for k := range flags {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, k := range keys {
		args = append(args, "-D", k+"="+flags[k])
	}

	run := &YmakeConfRun{cmd: exec.Command("python3", args...), script: script}

	run.cmd.Env = ymakeConfEnv()
	run.cmd.Stdout = &run.stdout
	run.cmd.Stderr = &run.stderr

	throw(run.cmd.Start())

	return run
}

func ymakeConfEnv() []string {
	env := []string{"Y_PYTHON_ENTRY_POINT=:main"}

	for _, k := range ymakeConfEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}

	return env
}

func (r *YmakeConfRun) wait() (map[string]string, []string) {
	if err := r.cmd.Wait(); err != nil {
		throwFmt("Config was not generated due to errors in %s (%v)\n%s", r.script, err, r.stderr.String())
	}

	if r.stderr.Len() > 0 {
		fmt.Fprintf(os.Stderr, "Non-empty stderr from ymake_conf.py:\n%s", r.stderr.String())
	}

	return parseYmakeConf(r.stdout.Bytes())
}

func parseYmakeConf(out []byte) (map[string]string, []string) {
	vars := map[string]string{}
	depth := 0

	var imports []string

	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)

		if depth == 0 {
			if strings.HasPrefix(trimmed, "@import") {
				imports = append(imports, confImportPath(ymakeConfScript, trimmed))

				continue
			}

			if trimmed == "" || trimmed[0] == '#' || trimmed[0] == '@' {
				continue
			}

			if name, value, appendOp, ok := confAssignment(line); ok {
				if prev := vars[name]; appendOp && prev != "" {
					if value != "" {
						value = prev + " " + value
					} else {
						value = prev
					}
				}

				vars[name] = value

				continue
			}
		}

		depth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")
		depth = max(depth, 0)
	}

	return vars, imports
}

func confAssignment(line string) (name, value string, appendOp, ok bool) {
	eq := strings.IndexByte(line, '=')

	if eq <= 0 {
		return "", "", false, false
	}

	name = line[:eq]

	if strings.HasSuffix(name, "+") {
		name = name[:len(name)-1]
		appendOp = true
	}

	if name == "" || !isConfVarName(name) {
		return "", "", false, false
	}

	return name, line[eq+1:], appendOp, true
}

func isConfVarName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]

		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}

	return true
}
