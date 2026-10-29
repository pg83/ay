package main

import "path"

type ConfigurationDependency struct {
	from VFS
	to   string
}

type ConfigurationDependencies struct {
	recurses   map[VFS][]string
	depends    []ConfigurationDependency
	processing bool
}

func appendConfigurationRecurses(out []string, dir string, stmt UnknownStmt, platform *Platform) []string {
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
				out = append(out, internJoined(dir, child).string())

				continue
			}

			child = path.Join(dir, child)
		}

		out = append(out, path.Clean(child))
	}

	return out
}

func (ctx *GenCtx) recordConfiguration(instance ModuleInstance, recurses, depends []string) {
	if instance.Platform == ctx.host || !instance.Platform.ifEnv.bool(envTraverseDepends) {
		return
	}

	configuration := &ctx.configuration

	if !configuration.processing {
		if configuration.recurses == nil {
			configuration.recurses = map[VFS][]string{}
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

	seen := map[string]bool{}

	var configure func(string)

	configure = func(dir string) {
		if seen[dir] || !ctx.fs.isFile(internStr(dir), "ya.make") {
			return
		}

		seen[dir] = true

		declaration := ctx.evaluateDir(dir, ctx.target).declaration

		if declaration != nil {
			root := ctx.roots[ctx.rootInstanceForDeclaration(dir, demandNone, declaration)]

			for _, instance := range ctx.testDependencyInstances(dir) {
				result := genModule(ctx, instance)

				if root {
					ctx.testRoots = append(ctx.testRoots, result)
				}
			}
		}

		for _, child := range configuration.recurses[source(dir)] {
			configure(child)
		}
	}

	for i := 0; i < len(configuration.depends); i++ {
		dependency := configuration.depends[i]

		configure(dependency.to)

		retain := ctx.peerTests

		for root := range ctx.roots {
			retain = retain || root.Path == dependency.from
		}

		if retain {
			ctx.testBuildDependencies(dependency.to)
		}
	}
}
