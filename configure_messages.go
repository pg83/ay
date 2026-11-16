package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	treePluginMacrosOnce sync.Once
	treePluginMacros     map[string]bool
	pluginMacroDef       = regexp.MustCompile(`(?m)^def on([a-z0-9_]+)\(`)
)

func vecDump(args []ANY) string {
	var b strings.Builder

	b.WriteString("vector of size ")
	b.WriteString(strconv.Itoa(len(args)))

	for i, arg := range args {
		b.WriteString("\n  ")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": ")
		b.WriteString(arg.string())
	}

	return b.String()
}

func ymakeVFS(v VFS) string {
	if rel := v.relString(); v.isSource() {
		return "$S/" + rel
	} else {
		return "$B/" + rel
	}
}

func unresolvedIncludeError(w Warn) string {
	return "could not resolve include file: [[imp]]" + w.target.string() + "[[rst]] included from here: [[imp]]" + ymakeVFS(w.includer) + "[[rst]]"
}

func testSizeError(size string) string {
	switch size {
	case "", "small", "medium", "large":
		return ""
	}

	return "Unknown test size: [[imp]]" + strings.ToUpper(size) + "[[rst]], choose from [[imp]]SMALL, MEDIUM, LARGE[[rst]]"
}

func treePluginMacro(fs FS, name STR) bool {
	if pluginMacroNames[name] {
		return true
	}

	treePluginMacrosOnce.Do(func() {
		treePluginMacros = map[string]bool{}

		for _, dir := range [...]string{"build/plugins", "build/internal/plugins"} {
			for _, packed := range fs.listdir(dirKey(dir)).names {
				file := STR(packed >> 1).string()

				if packed&1 != 0 || !strings.HasSuffix(file, ".py") {
					continue
				}

				for _, m := range pluginMacroDef.FindAllSubmatch(concatChunks(fs.read(dir+"/"+file)), -1) {
					treePluginMacros[strings.ToUpper(string(m[1]))] = true
				}
			}
		}
	})

	return treePluginMacros[name.string()]
}

func (ctx *GenCtx) missingDirectory(dir STR) string {
	if present, isDir := ctx.fs.existsID(srcRootRel, dir); present && isDir {
		return "directory without ya.make: "
	}

	return "missing directory: "
}
