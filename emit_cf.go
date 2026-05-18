package main

import (
	"regexp"
	"sort"
	"strings"
)

// emitExplicitCF emits a CF node for an explicit CONFIGURE_FILE(src dst)
// declaration (not triggered by a .cpp.in source in SRCS).
func emitExplicitCF(ctx *genCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *moduleData, reg *CodegenRegistry) {
	// Build a minimal ModuleCCInputs for the header-closure walk — only
	// the scanner context matters; the compilation flags are not used.
	in := ModuleCCInputs{
		DefaultVars:     d.defaultVars,
		DefaultVarOrder: d.defaultVarOrder,
		SourceRoot:      ctx.sourceRoot,
		FS:              ctx.fs,
	}

	srcPath := cf.Src
	if !strings.Contains(srcPath, "/") {
		srcPath = instance.Path + "/" + cf.Src
	}
	in.IncludeInputs = walkClosure(ctx, instance, resolveSourceVFS(ctx, instance, cf.Src, in.SrcDir), in)

	cfgVars := buildCFGVars(ctx.fs, instance.Path+"/"+cf.Src, d.defaultVars, d.defaultVarOrder)
	cfRef, cfOut := EmitCF(instance, cf.Src, cfgVars, in.IncludeInputs, ctx.emit)

	if reg != nil {
		registerBoundGeneratedParsedOutput(ctx, instance, "CF", cfOut, cfIncludeDirectives(ctx.parsers, instance.Path+"/"+cf.Src), cfRef)
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

// buildCFGVars filters the module's DEFAULT declarations to vars actually
// @VAR@-referenced in the .in template at rel, sorted alphabetically
// (ymake's order). @BUILD_TYPE@ without a DEFAULT falls back to DEBUG.
// Reads the template via fs at the walker layer — primitive emitters
// never touch FS.
func buildCFGVars(fs *FS, rel string, defaultVars map[string]string, defaultVarOrder []string) []string {
	referenced := map[string]bool{}

	if data, err := fs.Read(rel); err == nil {
		for _, m := range cfgVarRefRe.FindAllSubmatch(data, -1) {
			referenced[string(m[1])] = true
		}
	}

	var vars []string
	declaredSet := map[string]bool{}

	for _, name := range defaultVarOrder {
		if !referenced[name] {
			continue
		}

		val, ok := defaultVars[name]
		if !ok {
			continue
		}

		vars = append(vars, name+"="+val)
		declaredSet[name] = true
	}

	if referenced["BUILD_TYPE"] && !declaredSet["BUILD_TYPE"] {
		vars = append(vars, buildTypeDebug)
	}

	sort.Strings(vars)

	return vars
}
