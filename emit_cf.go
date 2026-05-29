package main

import (
	"regexp"
	"sort"
	"strings"
)

func emitExplicitCF(ctx *genCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *moduleData, reg *CodegenRegistry) {

	in := ModuleCCInputs{
		InclArgs:        ctx.inclArgs,
		Flags:           d.flags,
		DefaultVars:     d.defaultVars,
		DefaultVarOrder: d.defaultVarOrder,
		SetVars:         d.setVars,
		SourceRoot:      ctx.sourceRoot,
		FS:              ctx.fs,
	}

	srcVFS := copyFileInputVFS(ctx.fs, instance.Path, cf.Src)
	outVFS := copyFileOutputVFS(instance.Path, cf.Dst)
	in.IncludeInputs = walkClosure(ctx, instance, srcVFS, in)

	cfgVars := buildCFGVars(ctx.fs, srcVFS.Rel(), d.setVars, d.defaultVars)
	cfRef, cfOut := EmitCF(instance, srcVFS, outVFS, cfgVars, in.IncludeInputs, instance.Path, cfModuleTag(d, instance), ctx.emit)

	if reg != nil {

		parsed := []includeDirective{
			{kind: includeQuoted, target: internString(srcVFS.Rel())},
			{kind: includeQuoted, target: internString(configureFilePyVFS.Rel())},
		}
		parsed = append(parsed, cfIncludeDirectives(ctx.parsers, srcVFS.Rel())...)
		// Record CF source on the GeneratedFileInfo so antlr / similar
		// consumers can extend their inputs with srcVFS + configure_file.py
		// when an INFile is a CF output (upstream tracks both as JV inputs).
		registerBoundGeneratedParsedOutputWithSource(ctx, instance, "CF", cfOut, srcVFS, parsed, cfRef)
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

var cfgVarRefRe = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)

var cfgCmakeDefineRe = regexp.MustCompile(`#cmakedefine(?:01)?[ \t]+([A-Z_][A-Z0-9_]*)`)

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
func cfModuleTag(d *moduleData, instance ModuleInstance) string {
	if d == nil || d.moduleStmt == nil {
		return ""
	}
	if d.moduleStmt.Name == "PROTO_LIBRARY" && instance.Language != LangPy {
		return "cpp_proto"
	}
	return ""
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
