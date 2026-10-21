package main

import (
	"path"
	"strings"
)

func collectDllFor(ctx *GenCtx, dir string, kind ModuleKind, language Language, stmt *UnknownStmt, env Environment, d *ModuleData, onWarn func(Warn)) {
	if len(stmt.Args) == 0 {
		throwFmt("DLL_FOR expects a source directory")
	}

	forDir := path.Clean(stmt.Args[0].string())

	if rooted, ok := moduleRootedVFS(dir, forDir); ok {
		if !rooted.isSource() {
			throwFmt("DLL_FOR expects a source directory, got %s", forDir)
		}

		forDir = rooted.relString()
	}

	if forDir == dir {
		throwFmt("DLL_FOR: the current directory (%s) can not be used as an argument", dir)
	}

	for _, active := range ctx.dllForStack {
		if active == forDir {
			throwFmt("DLL_FOR: recursive module inclusion of %s", forDir)
		}
	}

	ctx.dllForStack = append(ctx.dllForStack, forDir)

	defer func() {
		ctx.dllForStack = ctx.dllForStack[:len(ctx.dllForStack)-1]
	}()

	var args []ANY
	var exports ANY

	for i := 1; i < len(stmt.Args); i++ {
		if stmt.Args[i].string() == "EXPORTS" {
			if i+1 < len(stmt.Args) {
				i++
				exports = stmt.Args[i]
			}
		} else {
			args = append(args, stmt.Args[i])
		}
	}

	set := func(name, value string) Stmt {
		return &SetStmt{Name: name, NameEnv: internEnv(name), Value: value, Line: stmt.Line}
	}

	out := []Stmt{
		set("DLL_FOR", "yes"), set("DLL_FOR_DIR", forDir), set("DLL_FOR_ARGS", strings.Join(anyStrs(args), " ")),
		&SrcDirStmt{Dirs: []ANY{internStr(forDir).any()}, Line: stmt.Line},
		&AddInclStmt{AllPaths: []ANY{source(forDir).any()}, Line: stmt.Line},
	}

	if exports != 0 {
		out = append(out, set("EXPORTS_FILE", exports.string()))
	}

	out = appendDllForModule(out, ctx.parseFileCached(internJoined(forDir, "ya.make").string()), args)
	collectStmts(ctx, dir, kind, language, out, env, d, onWarn)
}

func appendDllForModule(out, stmts []Stmt, args []ANY) []Stmt {
	for _, stmt := range stmts {
		switch v := stmt.(type) {
		case *ModuleStmt:
			module := *v

			module.Name, module.Args, module.Schema = tokDll, args, false
			out = append(out, &module)
		case *IfStmt:
			branch := *v

			branch.Then = appendDllForModule(nil, v.Then, args)
			branch.Else = appendDllForModule(nil, v.Else, args)

			out = append(out, &branch)
		default:
			out = append(out, stmt)
		}
	}

	return out
}
