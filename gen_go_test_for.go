package main

import (
	"path"
	"strings"
)

func (d *ModuleData) withGoTestForStmts(ctx *GenCtx, dir string, stmt *UnknownStmt, visit func([]Stmt)) {
	if len(stmt.Args) == 0 {
		throwFmt("GO_TEST_FOR expects a source directory")
	}

	forDir := path.Clean(stmt.Args[0].string())

	if forDir == dir {
		throwFmt("GO_TEST_FOR: the current directory (%s) can not be used as an argument", dir)
	}

	contentOnly, contentDone := d.readModuleContentOnly, d.readModuleContentDone

	d.readModuleContentOnly, d.readModuleContentDone = true, false

	defer func() {
		d.readModuleContentOnly, d.readModuleContentDone = contentOnly, contentDone
	}()

	visit([]Stmt{
		&SetStmt{Name: "GO_TEST_IMPORT_PATH", NameEnv: envGO_TEST_IMPORT_PATH, Value: forDir, Line: stmt.Line},
		&SetStmt{Name: "_GO_IMPORT_PATH", NameEnv: env_GO_IMPORT_PATH, Value: forDir, Line: stmt.Line},
		&SrcDirStmt{Dirs: []ANY{internStr(forDir).any()}, Line: stmt.Line},
	})

	visit(ctx.parseFileCached(internJoined(forDir, "ya.make")))
}

func (d *ModuleData) moduleContentDeclaration(stmt *ModuleStmt) *ModuleStmt {
	if !d.readModuleContentOnly || stmt.Name == tokGoTest {
		return stmt
	}

	module := *stmt

	module.Name = tokGoTest

	return &module
}

func (d *ModuleData) skipModuleContentStmt(name TOK) bool {
	return d.readModuleContentOnly && (strings.HasPrefix(name.string(), "RECURSE") || strings.HasPrefix(name.string(), "PARTITIONED_RECURSE"))
}
