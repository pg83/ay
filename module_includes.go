package main

import (
	"path"
	"slices"
	"strings"
)

func (ctx *GenCtx) includeTarget(stmt *IncludeStmt, env Environment) STR {
	rel := expandSourceScalarVarRefView(stmt.Path, env)

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

	targetID := internStr(target)

	if !ctx.fs.isFileVFS(targetID.source()) {
		throwFmt("%s:%d: INCLUDE file %s not found", stmt.File, stmt.Line, target)
	}

	return targetID
}

func (ctx *GenCtx) withIncludedStmts(stmt *IncludeStmt, env Environment, visit func([]Stmt)) {
	key := expansionKey(stmt.id, expansionInclude)
	words, ok := expansionMemo.find(key, env)

	var targetID STR

	if ok {
		targetID = words[0].str()
	} else {
		targetID = ctx.includeTarget(stmt, env)

		expansionMemo.end(key, targetID.any())
	}

	stmts := ctx.parseFileCached(targetID)

	if ctx.parsedFiles[parsedFileKey(targetID)].IncludeOnce {
		if env.s.included[targetID] {
			return
		}

		if env.s.included == nil {
			env.s.included = make(map[STR]bool)
		}

		env.s.included[targetID] = true
	}

	if slices.Contains(ctx.includeStack, targetID) {
		throwFmt("%s:%d: INCLUDE cycle: %s -> %s", stmt.File, stmt.Line, strings.Join(strStrings(ctx.includeStack), " -> "), targetID.string())
	}

	ctx.includeStack = append(ctx.includeStack, targetID)

	defer func() { ctx.includeStack = ctx.includeStack[:len(ctx.includeStack)-1] }()

	visit(stmts)
}
