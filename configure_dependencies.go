package main

import "path"

type ConfigurationDependency struct {
	from VFS
	to   string
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

func (ctx *GenCtx) recordConfiguration(instance ModuleInstance, recurses []VFS, depends []string) {
	if instance.Platform == ctx.host || !instance.Platform.ifEnv.bool(envTraverseDepends) {
		return
	}

	configuration := &ctx.configuration

	if !configuration.processing {
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

		declaration := ctx.evaluateDirVFS(dir, ctx.target).declaration

		if declaration != nil {
			root := ctx.roots[ctx.rootInstanceForDeclarationVFS(dir, demandNone, declaration)]

			for _, instance := range ctx.testDependencyInstances(dir.relString()) {
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

		configure(source(path.Clean(dependency.to)))

		if ctx.peerTests || ctx.rootPaths[dependency.from] {
			ctx.testBuildDependencies(dependency.to)
		}
	}
}
