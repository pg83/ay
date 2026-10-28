package main

import (
	"path"
	"slices"
	"strings"
)

func (ctx *GenCtx) withIncludedStmts(stmt *IncludeStmt, env Environment, visit func([]Stmt)) {
	rel := expandSourceScalarVarRef(stmt.Path, env)

	var target string

	if v, rooted := moduleRootedVFS(path.Dir(stmt.File), rel); rooted {
		if !v.isSource() {
			throwFmt("%s:%d: INCLUDE expects a source file: %s", stmt.File, stmt.Line, rel)
		}

		target = v.relString()
	} else {
		if path.IsAbs(rel) {
			throwFmt("%s:%d: INCLUDE path escapes the source root: %s", stmt.File, stmt.Line, rel)
		}

		target = path.Join(path.Dir(stmt.File), rel)
	}

	if !ctx.fs.isFile(srcRootRel, target) {
		throwFmt("%s:%d: INCLUDE file %s not found", stmt.File, stmt.Line, target)
	}

	stmts := ctx.parseFileCached(internStr(target))

	if ctx.parsedFiles[target].IncludeOnce {
		if env.s.included[target] {
			return
		}

		if env.s.included == nil {
			env.s.included = make(map[string]bool)
		}

		env.s.included[target] = true
	}

	if slices.Contains(ctx.includeStack, target) {
		throwFmt("%s:%d: INCLUDE cycle: %s -> %s", stmt.File, stmt.Line, strings.Join(ctx.includeStack, " -> "), target)
	}

	ctx.includeStack = append(ctx.includeStack, target)

	defer func() { ctx.includeStack = ctx.includeStack[:len(ctx.includeStack)-1] }()

	visit(stmts)
}
