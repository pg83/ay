package main

import (
	"path"
	"slices"
	"strconv"
	"strings"
)

var partitionedRecurses = map[TOK]TOK{
	tokPartitionedRecurse:             tokRecurse,
	tokPartitionedRecurseForTests:     tokRecurseForTests,
	tokPartitionedRecurseRootRelative: tokRecurseRootRelative,
}

type ConfigurationDependency struct {
	from VFS
	to   STR
}

type ConfigurationDependencies struct {
	recurses   map[VFS][]VFS
	depends    []ConfigurationDependency
	processing bool
}

func appendConfigurationRecurses(out []VFS, dir string, stmt UnknownStmt, platform *Platform) []VFS {
	switch stmt.Name {
	case tokRecurse, tokRecurseRootRelative:
	case tokRecurseForTests:
		if !platform.ifEnv.bool(envTraverseRecurseForTests) {
			return out
		}
	default:
		return out
	}

	if value := platform.ifEnv.string(envTraverseRecurse); value != "" && !stringIsTruthy(value) {
		return out
	}

	for _, arg := range stmt.Args {
		child := arg.string()

		if stmt.Name != tokRecurseRootRelative {
			if pathIsClean(child) && child != "" {
				out = append(out, internJoined(dir, child).source())

				continue
			}

			child = path.Join(dir, child)
		} else if id := arg.str(); id != 0 && id.cleanPath() {
			out = append(out, id.source())

			continue
		}

		out = append(out, source(path.Clean(child)))
	}

	return out
}

func (d *ModuleData) recordRecurse(ctx *GenCtx, modulePath string, stmt UnknownStmt, env Environment, onWarn func(Warn), platform *Platform) {
	switch stmt.Name {
	case tokRecurse, tokRecurseForTests:
		if d.moduleStmt != nil && !d.moduleEnded {
			instance := ctx.rootInstanceForDeclaration(modulePath, demandNone, d.declared)

			instance.Platform = platform

			onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: "[[alt1]]" + stmt.Raw.string() + "[[rst]] inside module " + moduleNodeName(instance, &ModuleEmitResult{}, d.declared) + "; ymake skip this."})

			return
		}
	case tokPartitionedRecurse, tokPartitionedRecurseForTests, tokPartitionedRecurseRootRelative:
		stmt = ctx.partitionedRecurse(modulePath, stmt, env, onWarn)
	}

	d.recurses = appendConfigurationRecurses(d.recurses, modulePath, stmt, platform)

	for len(d.recurseKinds) < len(d.recurses) {
		d.recurseKinds = append(d.recurseKinds, stmt.Name)
	}
}

func (ctx *GenCtx) recordConfiguration(instance ModuleInstance, recurses []VFS, depends []STR) {
	if instance.Platform == ctx.host || !instance.Platform.ifEnv.bool(envTraverseDepends) {
		return
	}

	configuration := &ctx.configuration

	if !configuration.processing && len(recurses) != 0 {
		if configuration.recurses == nil {
			configuration.recurses = map[VFS][]VFS{}
		}

		configuration.recurses[instance.Path] = append(configuration.recurses[instance.Path], recurses...)
	}

	for _, dir := range depends {
		configuration.depends = append(configuration.depends, ConfigurationDependency{instance.Path, dir})
	}
}

func (ctx *GenCtx) configureDependencies() {
	configuration := &ctx.configuration

	configuration.processing = true

	defer func() { configuration.processing = false }()

	seen := map[VFS]bool{}

	var configure func(VFS)

	configure = func(dir VFS) {
		if seen[dir] || !ctx.fs.isFileID(dir.rel(), strYaMake) {
			return
		}

		seen[dir] = true

		declaration := ctx.dirDeclaration(dir, ctx.target)

		if declaration != nil {
			root := ctx.roots[ctx.rootInstanceForDeclarationVFS(dir, demandNone, declaration)]

			for _, instance := range ctx.testDependencyInstances(dir.rel()) {
				result := genModule(ctx, instance)

				if root {
					ctx.testRoots = append(ctx.testRoots, result)
				}
			}
		}

		for _, child := range configuration.recurses[dir] {
			configure(child)
		}
	}

	for i := 0; i < len(configuration.depends); i++ {
		dependency := configuration.depends[i]

		if msg := ctx.dependsDirError(dependency.to); msg != "" {
			ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dependency.from.relString()), Error: msg})

			continue
		}

		if ctx.blacklist != nil {
			if rule := ctx.blacklist.match(dependency.to.string()); rule != "" {
				ctx.reportBlacklisted(dependency.from.relString(), dependency.to.sourceClean(), rule, "DEPENDS")
			}
		}

		configure(dependency.to.sourceClean())

		if msg := ctx.dependsMultimoduleError(dependency.to); msg != "" {
			ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dependency.from.relString()), Error: msg})
		}

		if ctx.peerTests || ctx.rootPaths[dependency.from] {
			ctx.testBuildDependencies(dependency.to)
		}
	}
}

func allDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

func (ctx *GenCtx) partitionedRecurse(dir string, stmt UnknownStmt, env Environment, report func(Warn)) UnknownStmt {
	name := stmt.Name.string()
	out := stmt

	out.Name = partitionedRecurses[stmt.Name]
	out.Args = nil

	config := ""

	for i := 0; i < len(stmt.Args); i++ {
		if stmt.Args[i].string() == "BALANCING_CONFIG" {
			if i+1 < len(stmt.Args) {
				i++
				config = stmt.Args[i].string()
			}

			continue
		}

		out.Args = append(out.Args, stmt.Args[i])
	}

	index, count := env.string(envRecursePartitionIndex), env.string(envRecursePartitionsCount)

	if !allDigits(index) || !allDigits(count) {
		return out
	}

	at, _ := strconv.Atoi(index)
	n, _ := strconv.Atoi(count)

	if config != "" {
		rel := path.Clean(config)

		if !ctx.fs.isFilePathClean(rel) {
			report(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dir), Error: missingIncludeError(&MissingIncludeStmt{Path: rel, File: joinRel(dir, "ya.make")})})
		}

		if dirs, err := ctx.balancedPartition(rel, out.Args, at, n); err == "" {
			out.Args = dirs

			return out
		} else {
			row, col := ctx.statementEnd(dir, stmt.Line, name)

			report(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dir), Error: "Unable to load balancing config [[imp]]$S/" + rel + "[[rst]] inside [[alt1]]" + name + "[[rst]]: " + err, Row: row, Column: col})
		}
	}

	out.Args = recursePartition(out.Args, at, n)

	return out
}

func recursePartition(args []ANY, index, count int) []ANY {
	count = max(1, count)

	chunk := max(1, (len(args)+count-1)/count)
	start := index * chunk

	if start >= len(args) {
		return nil
	}

	return args[start:min(len(args), start+chunk)]
}

func (ctx *GenCtx) balancedPartition(rel string, args []ANY, index, count int) ([]ANY, string) {
	abs := ctx.fs.root() + "/" + rel

	switch present, isDir := ctx.fs.exists(srcRootRel, rel); {
	case !present:
		return nil, " Path does not exist: " + abs
	case isDir:
		return nil, " Path is not file: " + abs
	}

	root, err := readConfJSON(concatChunks(ctx.fs.read(rel)))

	if err != "" {
		return nil, " Unable to read json tree from " + abs
	}

	field := func(v *confJSON, key string) *confJSON {
		if v == nil || v.kind != confJSONObject {
			return nil
		}

		if i := slices.Index(v.keys, key); i >= 0 {
			return v.items[i]
		}

		return nil
	}

	partitions := field(field(root, "partitions"), strconv.Itoa(count))

	if partitions == nil {
		return nil, " partitions section of balancing config does not contain appropriate balancing for " + strconv.Itoa(count) + " partitions."
	}

	outside := map[string]bool{}

	for _, arg := range args {
		outside[arg.string()] = true
	}

	var dirs []ANY

	for i, key := range partitions.keys {
		at, _ := strconv.Atoi(key)

		for _, item := range partitions.items[i].items {
			if outside[item.str] && at == index {
				dirs = append(dirs, internStr(item.str).any())
			}

			delete(outside, item.str)
		}
	}

	var unmapped []ANY

	for _, arg := range args {
		if outside[arg.string()] {
			unmapped = append(unmapped, arg)
		}
	}

	return append(dirs, recursePartition(unmapped, index, count)...), ""
}
