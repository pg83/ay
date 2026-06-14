package main

import (
	"regexp"
	"sort"
	"strings"
)

var (
	cfgVarRefRe         = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)
	cfgCmakeDefineRe    = regexp.MustCompile(`#cmakedefine(?:01)?[ \t]+([A-Z_][A-Z0-9_]*)`)
	configureFilePyPath = configureFilePyVFS.string()
)

func emitExplicitCF(ctx *GenCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *ModuleData) {
	in := ModuleCCInputs{
		TC:              d.tc,
		InclArgs:        ctx.inclArgs,
		Flags:           d.flags,
		DefaultVars:     d.defaultVars,
		DefaultVarOrder: d.defaultVarOrder,
		SetVars:         d.setVars,
		SourceRoot:      ctx.sourceRoot,
		FS:              ctx.fs,
		ScanCfg:         newScanContext(ctx.parsers, nil, nil, includeScannerBasePaths(), instance.Path.rel()),
	}

	srcVFS := copyFileInputVFS(ctx.fs, instance.Path.rel(), cf.Src)
	outVFS := copyFileOutputVFS(instance.Path.rel(), cf.Dst)
	in.IncludeInputs = walkClosure(ctx.scannerFor(instance), srcVFS, in.ScanCfg)

	cfgVars := buildCFGVars(ctx.fs, srcVFS.rel(), d.setVars, d.defaultVars)
	cfRef, cfOut := emitCF(instance, srcVFS, outVFS, cfgVars, in.IncludeInputs, instance.Path.rel(), cfModuleTag(d, instance), in.TC, ctx.emit)

	parsed := []IncludeDirective{
		{kind: includeQuoted, target: internStr(srcVFS.rel())},
		{kind: includeQuoted, target: internStr(configureFilePyVFS.rel())},
	}
	parsed = append(parsed, cfIncludeDirectives(ctx.parsers, srcVFS.rel())...)
	// Record CF source on the GeneratedFileInfo so antlr / similar
	// consumers can extend their inputs with srcVFS + configure_file.py
	// when an INFile is a CF output (upstream tracks both as JV inputs).
	registerBoundGeneratedParsedOutputWithSource(ctx, instance, pkCF, cfOut, srcVFS, parsed, cfRef, nil)
}

func cfIncludeDirectives(pm *IncludeParserManager, rel string) []IncludeDirective {
	raw := pm.sourceParsedBuckets(source(rel), nil).bucket(parsedIncludesLocal)
	out := make([]IncludeDirective, 0, len(raw))

	for _, d := range raw {
		if d.kind != includeQuoted {
			continue
		}

		out = append(out, d)
	}

	if len(out) == 0 {
		return nil
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.string() < out[j].target.string() })

	return out
}

func buildCFGVars(fs FS, rel string, setVars, defaultVars map[STR]STR) []string {
	referenced := map[string]bool{}
	data := fs.read(rel)

	for _, m := range cfgVarRefRe.FindAllSubmatch(data, -1) {
		referenced[string(m[1])] = true
	}

	for _, m := range cfgCmakeDefineRe.FindAllSubmatch(data, -1) {
		referenced[string(m[1])] = true
	}

	var vars []string

	for name := range referenced {
		switch {
		case hasKey(setVars, internStr(name)):
			vars = append(vars, name+"="+cfgVarValue(setVars[internStr(name)].string()))
		case hasKey(defaultVars, internStr(name)):
			vars = append(vars, name+"="+cfgVarValue(defaultVars[internStr(name)].string()))
		case name == "BUILD_TYPE":
			vars = append(vars, buildTypeDebug)
		}
	}

	sort.Strings(vars)

	return vars
}

func hasKey(m map[STR]STR, k STR) bool {
	_, ok := m[k]

	return ok
}

// cfModuleTag returns the lowercased submodule tag (the upstream
// `MODULE_TAG` lowercased) for the CF node's TargetProperties. PROTO_LIBRARY
// in its CPP_PROTO instance lands under MODULE_TAG=CPP_PROTO (gen.go:557),
// which surfaces in REF dumps as `cpp_proto`. CF emits from other module
// types leave module_tag unset.
func cfModuleTag(d *ModuleData, instance ModuleInstance) STR {
	if d.moduleStmt.Name == tokProtoLibrary && instance.Language != LangPy {
		return tagCppProto
	}

	return 0
}

// cfgVarValue strips one outer pair of escaped double-quotes plus the
// surrounding literal `"…"` so that CONFIGURE_FILE's KEY=VALUE arg holds
// the bare grammar / cmake replacement value. The lexer keeps `\"` literal
// inside `"…"` so a SET(K "\"raw\"") line lands here as `\"raw\"`; that
// shape would be passed verbatim to configure_file.py, polluting the
// substituted file with stray quote chars.
func cfgVarValue(v string) string {
	if len(v) >= 4 && strings.HasPrefix(v, `\"`) && strings.HasSuffix(v, `\"`) {
		v = v[2 : len(v)-2]
	} else if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
		v = v[1 : len(v)-1]
	}

	return v
}

const buildTypeDebug = "BUILD_TYPE=DEBUG"

func emitCF(
	instance ModuleInstance,
	srcVFS VFS,
	outVFS VFS,
	cfgVars []string,
	includeInputs []VFS,
	moduleDir string,
	moduleTag STR,
	tc ModuleToolchain,
	emit Emitter,
) (NodeRef, VFS) {
	na := emit.nodeArenas()

	env := EnvVars{{Name: envARCADIA_ROOT_DISTBUILD, Value: strS}}

	cmdArgs := []STR{
		tc.Python3,
		(configureFilePyVFS).str(),
		(srcVFS).str(),
		(outVFS).str(),
	}
	cmdArgs = appendInternStrs(cmdArgs, cfgVars)

	node := &Node{
		Platform: instance.Platform,
		Cmds: na.cmdList(Cmd{CmdArgs: na.chunkList(cmdArgs),
			Env: env}),
		Env:     env,
		Inputs:  na.inputList(na.vfsList(configureFilePyVFS), includeInputs),
		KV:      KV{P: pkCF, PC: pcYellow},
		Outputs: na.vfsList(outVFS),
		TargetProperties: func() TargetProperties {
			tp := TargetProperties{ModuleDir: moduleDir}

			if moduleTag != 0 {
				tp.ModuleTag = moduleTag
			}

			return tp
		}(),
		Requirements: Requirements{CPU: float64(1), Network: nwRestricted, RAM: float64(32)},
		DepRefs:      []NodeRef{},
		Resources:    usesPython3,
	}

	return emit.emit(node), outVFS
}
