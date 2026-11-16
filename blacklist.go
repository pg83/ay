package main

import (
	"strconv"
	"strings"
)

type blacklistRule struct {
	blacklisted bool
	raw, file   string
	line        int
}

type Blacklist struct {
	root    *blacklistNode
	checked BitSet
}

type blacklistNode struct {
	children map[string]*blacklistNode
	rule     *blacklistRule
	path     string
}

func blacklistStatus(blacklisted bool) string {
	if blacklisted {
		return "positive"
	}

	return "inverse"
}

func normalizeBlacklistPath(p string) (string, bool) {
	if strings.HasPrefix(p, "/") {
		return "", false
	}

	var parts []string

	for _, c := range strings.Split(p, "/") {
		switch c {
		case "", ".":
		case "..":
			if len(parts) == 0 {
				return "", false
			}

			parts = parts[:len(parts)-1]
		default:
			parts = append(parts, c)
		}
	}

	return strings.Join(parts, "/"), len(parts) != 0
}

func (ctx *GenCtx) loadBlacklist(env Environment) *Blacklist {
	files := strings.Fields(env.expand(env.string(env_BLACKLISTS)))

	if len(files) == 0 {
		return nil
	}

	b := &Blacklist{root: &blacklistNode{}}

	report := func(msg string) {
		ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: "At top level", Error: msg})
	}

	for _, file := range files {
		if !ctx.fs.isFile(srcRootRel, file) {
			abs := ctx.fs.root() + "/" + file

			report("Error while reading blacklist file " + file + `: (Error 2: No such file or directory) util/system/file.cpp:936: can't open "` + abs + `" with mode RdOnly|Seq (0x00000028)`)

			continue
		}

		own := map[string]blacklistRule{}
		blocked := false
		content := strings.ReplaceAll(string(concatChunks(ctx.fs.read(file))), "\r\n", "\n")

		for no, line := range strings.Split(strings.ReplaceAll(content, "\r", "\n"), "\n") {
			if hash := strings.IndexByte(line, '#'); hash >= 0 {
				line = line[:hash]
			}

			raw := strings.TrimSpace(line)

			if raw == "" {
				continue
			}

			path, blacklisted := raw, true

			if path[0] == '!' {
				path, blacklisted = strings.TrimSpace(path[1:]), false
			}

			normalized, ok := normalizeBlacklistPath(path)

			if !ok {
				continue
			}

			current := blacklistRule{blacklisted: blacklisted, raw: raw, file: file, line: no + 1}

			if previous, ok := own[normalized]; ok && previous.blacklisted != blacklisted {
				report("Conflicting blacklist rule for normalized path [[alt1]]" + normalized + "[[rst]]: [[imp]]" + file + ":" + strconv.Itoa(current.line) + "[[rst]] declares " + blacklistStatus(blacklisted) + " rule [[alt1]]" + raw + "[[rst]]; earlier declaration at [[imp]]" + previous.file + ":" + strconv.Itoa(previous.line) + "[[rst]] is " + blacklistStatus(previous.blacklisted) + " rule [[alt1]]" + previous.raw + "[[rst]]. The blacklist file will not be applied.")

				blocked = true

				continue
			}

			own[normalized] = current
		}

		if !blocked {
			for path, rule := range own {
				b.add(path, rule)
			}
		}
	}

	return b
}

func (b *Blacklist) add(path string, rule blacklistRule) {
	node := b.root

	for _, c := range strings.Split(path, "/") {
		child := node.children[c]

		if child == nil {
			if node.children == nil {
				node.children = map[string]*blacklistNode{}
			}

			child = &blacklistNode{}
			node.children[c] = child
		}

		node = child
	}

	node.rule, node.path = &rule, path
}

func (b *Blacklist) match(path string) string {
	verdict := ""

	for node := b.root; node != nil && path != ""; {
		c, rest, _ := strings.Cut(path, "/")

		if node = node.children[c]; node != nil && node.rule != nil {
			verdict = ""

			if node.rule.blacklisted {
				verdict = node.path
			}
		}

		path = rest
	}

	return verdict
}

func blacklistError(path, rule, macro string) string {
	inside := ""

	if macro != "" {
		inside = "inside [[alt1]]" + macro + "[[rst]] "
	}

	return "Path [[imp]]" + path + "[[rst]] " + inside + "is from prohibited directory [[alt1]]" + rule + "[[rst]]"
}

func (ctx *GenCtx) reportBlacklisted(dir string, v VFS, rule, macro string) {
	ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dir), Error: blacklistError(ymakeVFS(v), rule, macro)})
}

func (ctx *GenCtx) checkModuleBlacklist(instance ModuleInstance, d *ModuleData, peers []ResolvedPeer) {
	b := ctx.blacklist

	if b == nil || instance.Platform == ctx.host {
		return
	}

	dir := instance.Path.relString()

	check := func(v VFS, macro string) {
		if rule := b.match(v.relString()); rule != "" {
			ctx.reportBlacklisted(dir, v, rule, macro)
		}
	}

	for _, peer := range peers {
		if rule := peer.result.blacklistRule; rule != 0 {
			ctx.reportBlacklisted(dir, peer.path.source(), rule.string(), "PEERDIR")
		}
	}

	for _, src := range d.srcDirs {
		if src != 0 && src != instance.Path {
			check(src, "SRCDIR")
		}
	}

	for _, list := range [...][]VFS{d.addIncl, d.addInclGlobal, d.addInclUserGlobal, d.addInclOneLevel} {
		for _, incl := range list {
			check(incl, "ADDINCL")
		}
	}

	for _, data := range d.test.data {
		if rel, ok := strings.CutPrefix(data, "arcadia/"); ok {
			check(source(strings.TrimRight(rel, "/")), "DATA/DATA_FILES")
		}
	}
}

func (ctx *GenCtx) scannedFile(v VFS, children []VFS) {
	if ctx.blacklist != nil {
		ctx.blacklistFile(v)
	}

	if ctx.isolated != nil && len(children) != 0 {
		ctx.isolatedIncludes(v, children)
	}
}

func (ctx *GenCtx) blacklistFile(v VFS) {
	b := ctx.blacklist

	if b.checked.has(uint32(v)) || !v.isSource() || ctx.whereModule == 0 {
		return
	}

	b.checked.add(uint32(v))

	if rule := b.match(v.relString()); rule != "" {
		ctx.reportBlacklisted(ctx.whereModule.relString(), v, rule, "")
	}
}

func blacklistRecurseMacro(name TOK) string {
	if name == tokRecurseForTests {
		return "RECURSE_FOR_TESTS"
	}

	return "RECURSE/RECURSE_ROOT_RELATIVE"
}
