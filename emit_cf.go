package main

import (
	"regexp"
	"sort"
)

// emitExplicitCF emits a CF node for an explicit CONFIGURE_FILE(src dst)
// declaration (not triggered by a .cpp.in source in SRCS).
func emitExplicitCF(ctx *genCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *moduleData, reg *CodegenRegistry) {
	// Build a minimal ModuleCCInputs for the header-closure walk — only
	// the scanner context matters; the compilation flags are not used.
	in := ModuleCCInputs{
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

	cfgVars := buildCFGVars(ctx.fs, srcVFS.Rel, d.setVars, d.defaultVars)
	cfRef, cfOut := EmitCF(instance, srcVFS, outVFS, cfgVars, in.IncludeInputs, instance.Path, ctx.emit)

	if reg != nil {
		// The generated header carries its generation inputs (the .in
		// template + configure_file.py) as parsed includes, so every CC
		// that #includes it inherits them in its input closure — matching
		// the SRCS .cpp.in/.h.in path (emit_sources.go) and ymake's
		// transitive-input semantics. The template's own quoted #includes
		// follow (empty for pure @VAR@ headers like config.h /
		// protocol_version_variables.h).
		parsed := []includeDirective{
			{kind: includeQuoted, target: srcVFS.Rel},
			{kind: includeQuoted, target: configureFilePyVFS.Rel},
		}
		parsed = append(parsed, cfIncludeDirectives(ctx.parsers, srcVFS.Rel)...)
		registerBoundGeneratedParsedOutput(ctx, instance, "CF", cfOut, parsed, cfRef)
	}
}

// cfIncludeDirectives returns the `#include "..."` directives of a
// configure_file template (.cpp.in / .c.in) by reading the c parser's
// already-cached `parsedIncludesLocal` bucket and filtering to quoted
// entries (system includes resolve via the compiler search path, not
// via the codegen registry).
func cfIncludeDirectives(pm *includeParserManager, rel string) []includeDirective {
	raw := pm.sourceParsedBuckets(rel).bucket(parsedIncludesLocal)
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
	sort.Slice(out, func(i, j int) bool { return out[i].target < out[j].target })
	return out
}

// cfgVarRefRe matches @VAR_NAME@ substitution markers in .in template files.
var cfgVarRefRe = regexp.MustCompile(`@([A-Z_][A-Z0-9_]*)@`)

// cfgCmakeDefineRe matches `#cmakedefine VAR` and `#cmakedefine01 VAR`
// markers (configure_file.py handles both). ymake scans the template
// textually for $CFG_VARS, so it picks these up regardless of any
// surrounding `#ifdef` (e.g. config.h.in's `#ifdef __x86_64__` guard).
var cfgCmakeDefineRe = regexp.MustCompile(`#cmakedefine(?:01)?[ \t]+([A-Z_][A-Z0-9_]*)`)

// buildCFGVars filters the module's SET/DEFAULT declarations to vars
// actually referenced in the .in template at rel — as `@VAR@` or
// `#cmakedefine[01] VAR` — and emits `NAME=value` sorted alphabetically
// (ymake's $CFG_VARS order). SET overrides DEFAULT. @BUILD_TYPE@ with no
// declaration falls back to DEBUG. Reads the template via fs at the
// walker layer — primitive emitters never touch FS.
func buildCFGVars(fs *FS, rel string, setVars, defaultVars map[string]string) []string {
	referenced := map[string]bool{}

	if data, err := fs.Read(rel); err == nil {
		for _, m := range cfgVarRefRe.FindAllSubmatch(data, -1) {
			referenced[string(m[1])] = true
		}
		for _, m := range cfgCmakeDefineRe.FindAllSubmatch(data, -1) {
			referenced[string(m[1])] = true
		}
	}

	var vars []string
	for name := range referenced {
		switch {
		case hasKey(setVars, name):
			vars = append(vars, name+"="+setVars[name])
		case hasKey(defaultVars, name):
			vars = append(vars, name+"="+defaultVars[name])
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
