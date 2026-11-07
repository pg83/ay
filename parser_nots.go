package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

var ()

const (
	notsERMPath   = "devtools/frontend_build_platform/erm/erm-packages.json"
	notsTier0Path = "devtools/frontend_build_platform/nots/constants/src/tier0-settings.yaml"
)

type NotsPackage struct {
	Path string
	URI  string
}

type NotsLockfile struct {
	Packages []NotsPackage
}

type ParsedJSON struct {
	Value any
	Error *Exception
}

func (pm *IncludeParserManager) parsedJSON(rel string) ParsedJSON {
	return pm.parsedJSONID(internStr(rel))
}

func (pm *IncludeParserManager) parsedJSONID(key STR) ParsedJSON {
	if v, ok := pm.cache.notsJSON.get(key); ok {
		return v
	}

	var v ParsedJSON

	v.Error = try(func() { throw(json.Unmarshal(concatChunks(pm.fs.readPath(key)), &v.Value)) })

	pm.cache.notsJSON.put(key, v)

	return v
}

func (pm *IncludeParserManager) usrvDeps(rel string) *usrvDepsIndex {
	return pm.usrvDepsID(internStr(rel))
}

func (pm *IncludeParserManager) usrvDepsID(key STR) *usrvDepsIndex {
	if v, ok := pm.cache.usrvDeps.get(key); ok {
		return v
	}

	data := concatChunks(pm.fs.readPath(key))

	var index *usrvDepsIndex

	if !json.Valid(data) || data[jsonSkipSpace(data, 0)] != '{' {
		pm.notsJSONID(key)

		index = &usrvDepsIndex{}
	} else {
		index = newUsrvDepsIndex(data)
	}

	pm.cache.usrvDeps.put(key, index)

	return index
}

func (pm *IncludeParserManager) notsJSON(rel string) map[string]any {
	return pm.notsJSONID(internStr(rel))
}

func (pm *IncludeParserManager) notsJSONID(key STR) map[string]any {
	parsed := pm.parsedJSONID(key)

	if parsed.Error != nil {
		parsed.Error.throw()
	}

	if parsed.Value == nil {
		return nil
	}

	object, ok := parsed.Value.(map[string]any)

	if !ok {
		throwFmt("expected JSON object in %s", key.string())
	}

	return object
}

func (pm *IncludeParserManager) notsYAML(rel string) map[string]any {
	key := internStr(rel)

	if v, ok := pm.cache.notsYAML.get(key); ok {
		return v
	}

	v := map[string]any{}

	throw(yaml.Unmarshal(concatChunks(pm.fs.readPath(key)), &v))
	pm.cache.notsYAML.put(key, v)

	return v
}

func notsObject(v any) map[string]any {
	obj, _ := v.(map[string]any)

	return obj
}

func notsString(v any) string {
	s, _ := v.(string)

	return s
}

func notsDependencies(pj map[string]any) []map[string]any {
	return []map[string]any{notsObject(pj["dependencies"]), notsObject(pj["devDependencies"]), notsObject(pj["peerDependencies"]), notsObject(pj["optionalDependencies"]), notsObject(notsObject(pj["pnpm"])["overrides"])}
}

func notsHasDependencies(pj map[string]any) bool {
	for _, deps := range notsDependencies(pj) {
		if len(deps) > 0 {
			return true
		}
	}

	return false
}

func (pm *IncludeParserManager) notsPeers(dir string, pj map[string]any) []string {
	var peers []string

	for _, deps := range notsDependencies(pj) {
		for _, value := range deps {
			spec := notsString(value)
			rel, workspace := strings.CutPrefix(spec, "workspace:")

			if workspace {
				if !strings.HasPrefix(rel, ".") {
					throwFmt("%s/package.json: expected relative workspace dependency, got %q", dir, spec)
				}
			} else {
				var file bool
				rel, file = strings.CutPrefix(spec, "file:")

				if !file || path.IsAbs(rel) || !pm.fs.isFilePathClean(path.Join(dir, rel, "ya.make")) {
					continue
				}
			}

			peers = append(peers, path.Join(dir, rel))
		}
	}

	slices.Sort(peers)

	return slices.Compact(peers)
}

func notsVersionParts(s string) []int {
	parts := strings.Split(s, ".")

	if len(parts) != 3 {
		throwFmt("nots: invalid stable version %q", s)
	}

	out := make([]int, 3)

	for i, p := range parts {
		out[i] = throw2(strconv.Atoi(p))
	}

	return out
}

func (pm *IncludeParserManager) notsVersion(tool, constraint string) string {
	resource := notsObject(pm.notsJSON(notsERMPath)[tool])

	if constraint == "" {
		version := notsString(resource["default"])

		if version == "" {
			throwFmt("nots: no default version for %s in %s", tool, notsERMPath)
		}

		return version
	}

	os, oe, vs, ve, ok := notsVersionRangeMatch(constraint)

	if !ok {
		throwFmt("nots: unsupported version range %q", constraint)
	}

	operator := constraint[os:oe]
	want := notsVersionParts(constraint[vs:ve])
	selected := ""

	for version := range notsObject(resource["versions"]) {
		cmp := slices.Compare(notsVersionParts(version), want)

		if cmp != 0 && !(operator == ">=" && cmp > 0) {
			continue
		}

		if selected == "" || slices.Compare(notsVersionParts(version), notsVersionParts(selected)) < 0 {
			selected = version
		}
	}

	if selected == "" {
		throwFmt("nots: no %s version satisfies %q", tool, constraint)
	}

	return selected
}

type notsLockEntry struct {
	name, typ, tarball, integrity string
}

func (pm *IncludeParserManager) notsLockfile(rel string) *NotsLockfile {
	key := internStr(rel)

	if v, ok := pm.cache.notsLocks.get(key); ok {
		return v
	}

	data := concatChunks(pm.fs.readPath(key))
	version, entries, ok := notsLockfileScan(data)

	if !ok {
		version, entries = notsLockfileDecode(data)
	}

	if version != "6.0" && version != "9.0" {
		throwFmt("%s: unsupported lockfileVersion %q", rel, version)
	}

	result := &NotsLockfile{}
	seen := make(map[string]bool)
	registry := ""

	for _, item := range entries {
		name := item.name
		res := struct{ Type, Tarball, Integrity string }{item.typ, item.tarball, item.integrity}

		if res.Type == "directory" {
			continue
		}

		tarball := res.Tarball

		if tarball == "" {
			id, _, _ := strings.Cut(strings.TrimLeft(name, "/"), "(")
			pos := strings.LastIndexByte(id, '@')

			if pos <= 0 || !notsPackageKeyValid(id[:pos]) || !notsPackageVersionValid(id[pos+1:]) {
				throwFmt("%s: cannot infer tarball URL from %q", rel, name)
			}

			if registry == "" {
				registry = notsString(notsObject(notsObject(pm.notsYAML(notsTier0Path)["pnpm"])["defaults"])["registry"])
			}

			tarball = strings.TrimRight(registry, "/") + "/" + strings.ReplaceAll(id[:pos], "/", "%2f") + "/-/" + path.Base(id[:pos]) + "-" + id[pos+1:] + ".tgz"
		}

		tarball, _, _ = strings.Cut(tarball, "?")

		if strings.HasPrefix(tarball, "file:") {
			throwFmt("%s: tarball cannot point to a file: %s", rel, tarball)
		}

		if (strings.HasPrefix(tarball, "https://") || strings.HasPrefix(tarball, "http://")) && !strings.HasPrefix(tarball, "https://npm.yandex-team.ru/") && !strings.HasPrefix(tarball, "http://npm.yandex-team.ru/") {
			throwFmt("%s: tarball must use npm.yandex-team.ru: %s", rel, tarball)
		}

		algo, hash, found := strings.Cut(res.Integrity, "-")

		if !found || algo != "sha1" && algo != "sha512" {
			throwFmt("%s: invalid integrity %q", rel, res.Integrity)
		}

		throw2(base64.StdEncoding.DecodeString(hash))

		u := throw2(url.Parse(tarball))
		filename := path.Base(u.Path)
		packageEnd := strings.LastIndex(u.Path, "/-/")
		local := ""

		if packageEnd >= 0 {
			packagePath := u.Path[:packageEnd]
			parts := strings.Split(strings.TrimRight(packagePath, "/"), "/")

			local = parts[len(parts)-1]

			if len(parts) > 1 && strings.HasPrefix(parts[len(parts)-2], "@") {
				local = parts[len(parts)-2] + "/" + local
			}

			local += "/-/" + filename
		} else {
			parts := strings.Split(u.Path, "/")

			local = strings.Join(parts[max(0, len(parts)-3):], "/")
		}

		rawPath := u.EscapedPath()

		if sep := strings.LastIndex(rawPath, "/-/"); sep >= 0 && strings.Contains(rawPath[sep+3:], "/") {
			u.RawPath = rawPath[:sep+3] + path.Base(rawPath)
			u.Path = throw2(url.PathUnescape(u.RawPath))

			tarball = u.String()
		}

		if !strings.HasPrefix(tarball, "https://") && !strings.HasPrefix(tarball, "http://") {
			tarball = "https://npm.yandex-team.ru/" + tarball
		}

		if !seen[local] {
			result.Packages = append(result.Packages, NotsPackage{Path: local, URI: tarball + "#integrity=" + res.Integrity})
			seen[local] = true
		}
	}

	pm.cache.notsLocks.put(key, result)

	return result
}

func notsLockfileDecode(data []byte) (string, []notsLockEntry) {
	var doc struct {
		Version  string        `yaml:"lockfileVersion"`
		Packages yaml.MapSlice `yaml:"packages"`
	}
	throw(yaml.Unmarshal(data, &doc))

	entries := make([]notsLockEntry, 0, len(doc.Packages))

	for _, item := range doc.Packages {
		resolution := notsObject(notsObject(item.Value)["resolution"])

		entries = append(entries, notsLockEntry{
			name: notsString(item.Key), typ: notsString(resolution["type"]),
			tarball: notsString(resolution["tarball"]), integrity: notsString(resolution["integrity"]),
		})
	}

	return doc.Version, entries
}

func notsLockfileScan(data []byte) (string, []notsLockEntry, bool) {
	version := ""
	entries := []notsLockEntry(nil)
	inPackages := false

	for len(data) != 0 {
		line := data
		next := []byte(nil)

		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, next = data[:i], data[i+1:]
		}

		data = next
		line = bytes.TrimRight(line, " \r")

		if len(line) == 0 {
			continue
		}

		if line[0] != ' ' {
			inPackages = false

			if rest, found := bytes.CutPrefix(line, []byte("lockfileVersion:")); found {
				value, ok := notsLockScalar(bytes.TrimSpace(rest))

				if !ok {
					return "", nil, false
				}

				version = value
			} else if string(line) == "packages:" {
				inPackages = true
			} else if !notsLockTopKey(line) {
				return "", nil, false
			}

			continue
		}

		if !inPackages {
			continue
		}

		if bytes.HasPrefix(line, []byte("    ")) {
			if bytes.HasPrefix(line, []byte("    resolution:")) && len(entries) != 0 {
				if !notsLockResolution(&entries[len(entries)-1], bytes.TrimSpace(line[len("    resolution:"):])) {
					return "", nil, false
				}
			}

			continue
		}

		if !bytes.HasPrefix(line, []byte("  ")) || line[2] == ' ' || line[len(line)-1] != ':' {
			return "", nil, false
		}

		name, ok := notsLockScalar(line[2 : len(line)-1])

		if !ok {
			return "", nil, false
		}

		entries = append(entries, notsLockEntry{name: name})
	}

	return version, entries, true
}

func notsLockResolution(entry *notsLockEntry, flow []byte) bool {
	if len(flow) < 2 || flow[0] != '{' || flow[len(flow)-1] != '}' {
		return false
	}

	for item := range bytes.SplitSeq(flow[1:len(flow)-1], []byte(",")) {
		item = bytes.TrimSpace(item)

		if len(item) == 0 {
			continue
		}

		k, v, found := bytes.Cut(item, []byte(":"))

		if !found {
			return false
		}

		value, ok := notsLockScalar(bytes.TrimSpace(v))

		if !ok || strings.ContainsAny(value, "{}[]") {
			return false
		}

		switch string(bytes.TrimSpace(k)) {
		case "integrity":
			entry.integrity = value
		case "tarball":
			entry.tarball = value
		case "type":
			entry.typ = value
		case "directory", "commit", "repo", "path":
		default:
			return false
		}
	}

	return true
}

func notsLockScalar(s []byte) (string, bool) {
	if len(s) == 0 {
		return "", true
	}

	if s[0] == '\'' {
		if len(s) < 2 || s[len(s)-1] != '\'' {
			return "", false
		}

		body := s[1 : len(s)-1]

		if bytes.IndexByte(body, '\'') < 0 {
			return string(body), true
		}

		if bytes.Contains(bytes.ReplaceAll(body, []byte("''"), nil), []byte("'")) {
			return "", false
		}

		return string(bytes.ReplaceAll(body, []byte("''"), []byte("'"))), true
	}

	if s[0] == '"' || s[0] == '|' || s[0] == '>' || s[0] == '&' || s[0] == '*' || s[0] == '!' || s[0] == '%' || s[0] == '@' || s[0] == '`' || bytes.Contains(s, []byte(" #")) {
		return "", false
	}

	return string(s), true
}

func notsLockTopKey(line []byte) bool {
	colon := bytes.IndexByte(line, ':')

	if colon <= 0 || !isLockKeyByte(line[0]) || line[0] >= '0' && line[0] <= '9' {
		return false
	}

	for _, b := range line[1:colon] {
		if !isLockKeyByte(b) && b != '-' && b != '.' {
			return false
		}
	}

	return true
}

func isLockKeyByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
