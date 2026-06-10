package main

import (
	"regexp"
	"sort"
	"strings"
)

var (
	cfgVarRefRe      = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)
	cfgCmakeDefineRe = regexp.MustCompile(`#cmakedefine(?:01)?[ \t]+([A-Z_][A-Z0-9_]*)`)
)

func emitExplicitCF(ctx *genCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *moduleData, reg *CodegenRegistry) {
	in := ModuleCCInputs{
		TC:              d.tc,
		InclArgs:        ctx.inclArgs,
		Flags:           d.flags,
		DefaultVars:     d.defaultVars,
		DefaultVarOrder: d.defaultVarOrder,
		SetVars:         d.setVars,
		SourceRoot:      ctx.sourceRoot,
		FS:              ctx.fs,
	}

	srcVFS := copyFileInputVFS(ctx.fs, instance.Path.Rel(), cf.Src)
	outVFS := copyFileOutputVFS(instance.Path.Rel(), cf.Dst)
	in.IncludeInputs = walkClosure(ctx, instance, srcVFS, in)

	cfgVars := buildCFGVars(ctx.fs, srcVFS.Rel(), d.setVars, d.defaultVars)
	cfRef, cfOut := EmitCF(instance, srcVFS, outVFS, cfgVars, in.IncludeInputs, instance.Path.Rel(), cfModuleTag(d, instance), in.TC, ctx.emit)

	if reg != nil {
		parsed := []includeDirective{
			{kind: includeQuoted, target: internStr(srcVFS.Rel())},
			{kind: includeQuoted, target: internStr(configureFilePyVFS.Rel())},
		}
		parsed = append(parsed, cfIncludeDirectives(ctx.parsers, srcVFS.Rel())...)
		// Record CF source on the GeneratedFileInfo so antlr / similar
		// consumers can extend their inputs with srcVFS + configure_file.py
		// when an INFile is a CF output (upstream tracks both as JV inputs).
		registerBoundGeneratedParsedOutputWithSource(ctx, instance, pkCF, cfOut, srcVFS, parsed, cfRef, nil)
	}
}

func cfIncludeDirectives(pm *includeParserManager, rel string) []includeDirective {
	raw := pm.sourceParsedBuckets(Source(rel)).bucket(parsedIncludesLocal)
	out := make([]includeDirective, 0, len(raw))

	for _, d := range raw {
		if d.kind != includeQuoted {
			continue
		}

		out = append(out, d)
	}

	if len(out) == 0 {
		return nil
	}

	sort.Slice(out, func(i, j int) bool { return out[i].target.String() < out[j].target.String() })
	return out
}

func buildCFGVars(fs FS, rel string, setVars, defaultVars map[string]string) []string {
	referenced := map[string]bool{}
	data := fs.Read(rel)

	for _, m := range cfgVarRefRe.FindAllSubmatch(data, -1) {
		referenced[string(m[1])] = true
	}

	for _, m := range cfgCmakeDefineRe.FindAllSubmatch(data, -1) {
		referenced[string(m[1])] = true
	}

	var vars []string

	for name := range referenced {
		switch {
		case hasKey(setVars, name):
			vars = append(vars, name+"="+cfgVarValue(setVars[name]))
		case hasKey(defaultVars, name):
			vars = append(vars, name+"="+cfgVarValue(defaultVars[name]))
		case name == "BUILD_TYPE":
			vars = append(vars, buildTypeDebug)
		}
	}

	sort.Strings(vars)

	return vars
}

func hasKey(m map[string]string, k string) bool {
	_, ok := m[k]
	return ok
}

// cfModuleTag returns the lowercased submodule tag (the upstream
// `MODULE_TAG` lowercased) for the CF node's TargetProperties. PROTO_LIBRARY
// in its CPP_PROTO instance lands under MODULE_TAG=CPP_PROTO (gen.go:557),
// which surfaces in REF dumps as `cpp_proto`. CF emits from other module
// types leave module_tag unset.
func cfModuleTag(d *moduleData, instance ModuleInstance) STR {
	if d == nil || d.moduleStmt == nil {
		return 0
	}

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
