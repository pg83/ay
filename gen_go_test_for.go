package main

import (
	"path"
	"strings"
)

func (d *ModuleData) withGoTestForStmts(ctx *GenCtx, dir string, stmt *UnknownStmt, visit func([]Stmt)) {
	if len(stmt.Args) == 0 {
		throwFmt("GO_TEST_FOR expects a source directory")
	}

	forDirID := stmt.Args[0].textSTR()

	if !forDirID.cleanPath() {
		forDirID = internStr(path.Clean(forDirID.string()))
	}

	forDir := forDirID.string()

	if forDir == dir {
		throwFmt("GO_TEST_FOR: the current directory (%s) can not be used as an argument", dir)
	}

	contentOnly, contentDone := d.readModuleContentOnly, d.readModuleContentDone

	d.readModuleContentOnly, d.readModuleContentDone = true, false

	defer func() {
		d.readModuleContentOnly, d.readModuleContentDone = contentOnly, contentDone
	}()

	visit([]Stmt{
		newSetStmtID("GO_TEST_IMPORT_PATH", envGO_TEST_IMPORT_PATH, forDirID, stmt.Line),
		newSetStmtID("_GO_IMPORT_PATH", env_GO_IMPORT_PATH, forDirID, stmt.Line),
		&UnknownStmt{Name: tokSetAppend, Raw: tokSetAppend.str(), Args: []ANY{kwSRCDIR.any(), forDirID.any()}, Line: stmt.Line},
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
