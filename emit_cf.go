package main

import (
	"os"
	"sort"
	"strings"
)

// emitExplicitCF emits a CF node for an explicit CONFIGURE_FILE(src dst)
// declaration (not triggered by a .cpp.in source in SRCS).
func emitExplicitCF(ctx *genCtx, instance ModuleInstance, cf *ConfigureFileStmt, d *moduleData, reg *CodegenRegistry) {
	// Build a minimal ModuleCCInputs for CF emission — only DefaultVars
	// and the scanner context matter; the compilation flags are not used.
	in := ModuleCCInputs{
		DefaultVars:     d.defaultVars,
		DefaultVarOrder: d.defaultVarOrder,
		SourceRoot:      ctx.sourceRoot,
	}

	// Scan the .in file for its header closure (same as a .cpp source).
	srcPath := cf.Src
	if !strings.Contains(srcPath, "/") {
		srcPath = instance.Path + "/" + cf.Src
	}
	in.IncludeInputs = walkClosure(ctx, instance, resolveSourceVFS(ctx, instance, cf.Src, in.SrcDir), in)

	cfRef, cfOut := EmitCF(instance, cf.Src, in, ctx.emit)

	// Register the explicit CF output with EmitsIncludes.
	if reg != nil {
		diskPath := ctx.sourceRoot + "/" + instance.Path + "/" + cf.Src
		registerBoundGeneratedParsedOutput(ctx, instance, "CF", cfOut, cfIncludeDirectives(diskPath), cfRef)
	}
}

// cfIncludeDirectives parses `#include "..."` directives from a
// configure_file template (.cpp.in / .c.in). Quoted only (angle-bracket
// forms are system headers, resolved by the compiler search path).
// Returns directives sorted by target; nil on read failure.
//
// Legitimate disk read: extracts structured `#include` directives at
// registration time to populate EmitsIncludes. NOT for closure walks.
func cfIncludeDirectives(diskPath string) []includeDirective {
	data, err := os.ReadFile(diskPath)
	if err != nil {
		return nil
	}
	var out []includeDirective
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#include ") {
			continue
		}
		start := strings.IndexByte(t, '"')
		if start < 0 {
			continue
		}
		end := strings.IndexByte(t[start+1:], '"')
		if end < 0 {
			continue
		}
		inc := t[start+1 : start+1+end]
		if inc != "" {
			out = append(out, includeDirective{kind: includeQuoted, target: inc})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].target < out[j].target })
	return out
}
