package main

import "strings"

type CppStaticAnalyzer struct {
	config       VFS
	beforeSource []ANY
	afterSource  []ANY
	inputs       []VFS
	resources    []STR
}

func applySAPlugins(args []ANY, d *ModuleData, env Environment) {
	section := ""
	plugins := env.string(envClangSAPlugins)

	for _, arg := range args {
		value := arg.string()

		switch value {
		case "FROM", "NAME":
			section = value

			continue
		}

		switch section {
		case "FROM":
			markUserArg(arg)
			d.peerdirs = append(d.peerdirs, arg)
		case "NAME":
			markUserArg(arg)
			plugins += " ${" + value + resourceGlobalSuffix + "}/plugin.so"
		default:
			throwFmt("USE_SA_PLUGINS expects FROM and NAME sections")
		}
	}

	env.setString(envClangSAPlugins, strings.TrimSpace(plugins))
}

func (e *EmitContext) cppStaticAnalyzer(globals []ResourceDecl) *CppStaticAnalyzer {
	d := e.d

	if !d.clangSAEnable || d.clangSAConfig == "" {
		return nil
	}

	na := e.ctx.na
	root := ""
	resources := append([]STR(nil), e.instance.Platform.CCUsesResources...)

	for _, decl := range globals {
		if decl.Name == clangSAResource {
			root = decl.Value.string()
			resources = append(resources, decl.Name)
		}
	}

	plugins := resolveResourceGlobalArgs(d.clangSAPlugins, globals)

	for _, plugin := range plugins {
		for _, decl := range globals {
			if strings.HasPrefix(plugin.string(), decl.Value.string()+"/") {
				resources = append(resources, decl.Name)
			}
		}
	}

	config := copyFileInputVFS(e.ctx.fs, e.instance.Path, d.clangSAConfig)
	afterSource := na.anys.alloc(9 + len(plugins))[:0]

	afterSource = append(afterSource,
		strClangBin.any(), internStr(root+"/bin/clang").any(),
		strSourceRoot2.any(), argS.any(),
		strConfigFile.any(), config.any(),
		strPluginsBegin.any(), strDummyParam.any(),
	)

	afterSource = append(afterSource, plugins...)
	afterSource = append(afterSource, strPluginsEnd.any())
	na.anys.commit(len(afterSource))

	return &CppStaticAnalyzer{
		config:       config,
		beforeSource: na.anyList(d.tc.Python3.any(), clangSAScript.any(), strTestingSrc.any()),
		afterSource:  afterSource[:len(afterSource):len(afterSource)],
		inputs:       na.vfsList(clangSAScript, config),
		resources:    na.strs.list(dedup(resources)...),
	}
}

func (e *EmitContext) staticAnalyzerCmd(spec *CppStaticAnalyzer, in ModuleCCInputs, source []ANY, output []ANY) Cmd {
	e.codegen.use(spec.config)

	blocks := compilerCommandBlocks(in.CCBlocks, true)
	flags := blocks.flags[len(in.ClangWarnings):]

	return Cmd{
		CmdArgs: e.ctx.na.chunkList(spec.beforeSource, source, spec.afterSource,
			e.instance.Platform.CCHead, output, blocks.includes, flags, blocks.cxxTail,
			in.PerSourceCFlags),
		Env: envVarsVCS,
	}
}
