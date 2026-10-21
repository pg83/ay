package main

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

var (
	notsVersionRange   = regexp.MustCompile(`^\s*(>=|=)?\s*(\d+\.\d+\.\d+)\s*$`)
	notsPackageKey     = regexp.MustCompile(`^(?:@[0-9A-Za-z._~!'()*-]+/)?[0-9A-Za-z_~!'()*-][0-9A-Za-z._~!'()*-]*$`)
	notsPackageVersion = regexp.MustCompile(`^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

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
	key := internStr(rel)

	if v, ok := pm.cache.notsJSON.get(key); ok {
		return v
	}

	var v ParsedJSON

	v.Error = try(func() { throw(json.Unmarshal(concatChunks(pm.fs.readPath(key)), &v.Value)) })

	pm.cache.notsJSON.put(key, v)

	return v
}

func (pm *IncludeParserManager) notsJSON(rel string) map[string]any {
	parsed := pm.parsedJSON(rel)

	if parsed.Error != nil {
		parsed.Error.throw()
	}

	if parsed.Value == nil {
		return nil
	}

	object, ok := parsed.Value.(map[string]any)

	if !ok {
		throwFmt("expected JSON object in %s", rel)
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

	m := notsVersionRange.FindStringSubmatch(constraint)

	if m == nil {
		throwFmt("nots: unsupported version range %q", constraint)
	}

	want := notsVersionParts(m[2])
	selected := ""

	for version := range notsObject(resource["versions"]) {
		cmp := slices.Compare(notsVersionParts(version), want)

		if cmp != 0 && !(m[1] == ">=" && cmp > 0) {
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

func (pm *IncludeParserManager) notsLockfile(rel string) *NotsLockfile {
	key := internStr(rel)

	if v, ok := pm.cache.notsLocks.get(key); ok {
		return v
	}

	var doc struct {
		Version  string        `yaml:"lockfileVersion"`
		Packages yaml.MapSlice `yaml:"packages"`
	}
	throw(yaml.Unmarshal(concatChunks(pm.fs.readPath(key)), &doc))

	if doc.Version != "6.0" && doc.Version != "9.0" {
		throwFmt("%s: unsupported lockfileVersion %q", rel, doc.Version)
	}

	result := &NotsLockfile{}
	seen := make(map[string]bool)
	registry := ""

	for _, item := range doc.Packages {
		name := notsString(item.Key)
		resolution := notsObject(notsObject(item.Value)["resolution"])

		res := struct{ Type, Tarball, Integrity string }{
			notsString(resolution["type"]), notsString(resolution["tarball"]), notsString(resolution["integrity"]),
		}

		if res.Type == "directory" {
			continue
		}

		tarball := res.Tarball

		if tarball == "" {
			id, _, _ := strings.Cut(strings.TrimLeft(name, "/"), "(")
			pos := strings.LastIndexByte(id, '@')

			if pos <= 0 || !notsPackageKey.MatchString(id[:pos]) || !notsPackageVersion.MatchString(id[pos+1:]) {
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
