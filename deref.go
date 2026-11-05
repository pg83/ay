package main

func derefStmt(s Stmt, env Environment) Stmt {
	switch v := s.(type) {
	case *UnknownStmt:
		switch v.Name {
		case tokEnv, tokAddCustomExplicitLinterCheck, tokRun, tokUseRecipe, tokNoCheckImports, tokPySrcs, tokCopyFile, tokCopyFileWithContext:
			return s
		}

		if args, ok := derefList(v.Args, env); ok {
			c := *v

			c.Args = args

			return &c
		}
	case *PeerdirStmt:
		if paths, ok := derefList(v.Paths, env); ok {
			c := *v

			c.Paths = paths

			return &c
		}
	case *SrcsStmt:
		if sources, ok := derefList(v.Sources, env); ok {
			c := *v

			c.Sources = sources

			return &c
		}
	case *GlobalSrcsStmt:
		if sources, ok := derefList(v.Sources, env); ok {
			c := *v

			c.Sources = sources

			return &c
		}
	case *SrcDirStmt:
		if dirs, ok := derefList(v.Dirs, env); ok {
			c := *v

			c.Dirs = dirs

			return &c
		}
	case *JoinSrcsStmt:
		if sources, ok := derefList(v.Sources, env); ok {
			c := *v

			c.Sources = sources

			return &c
		}
	case *CFlagsStmt:
		if flags, ok := derefList(v.Flags, env); ok {
			c := *v

			c.Flags = flags

			return &c
		}
	case *CXXFlagsStmt:
		if flags, ok := derefList(v.Flags, env); ok {
			c := *v

			c.Flags = flags

			return &c
		}
	case *CONLYFlagsStmt:
		if flags, ok := derefList(v.Flags, env); ok {
			c := *v

			c.Flags = flags

			return &c
		}
	case *LDFlagsStmt:
		if flags, ok := derefList(v.Flags, env); ok {
			c := *v

			c.Flags = flags

			return &c
		}
	case *ResourceStmt:
		if args, ok := derefList(v.Args, env); ok {
			c := *v

			c.Args = args

			return &c
		}
	case *ResourceFilesStmt:
		if args, ok := derefList(v.Args, env); ok {
			c := *v

			c.Args = args

			return &c
		}
	case *AllResourceFilesStmt:
		if args, ok := derefList(v.Args, env); ok {
			c := *v

			c.Args = args

			return &c
		}
	case *DeclareResourceStmt:
		if args, ok := derefList(v.Args, env); ok {
			c := *v

			c.Args = args

			return &c
		}
	case *AddInclStmt:
		c := *v
		changed := false

		for _, list := range [...]*[]ANY{
			&c.GlobalPaths, &c.OneLevelPaths, &c.OwnPaths, &c.CythonPaths, &c.CythonGlobalPaths, &c.AsmPaths, &c.ProtoPaths,
			&c.ProtoGlobalPaths, &c.ProtoAllPaths, &c.FlatcPaths, &c.FlatcGlobalPaths, &c.SwigPaths, &c.SwigGlobalPaths,
			&c.ScPaths, &c.ScGlobalPaths, &c.ScExportPaths, &c.UserGlobalPaths, &c.AllPaths,
		} {
			if items, ok := derefList(*list, env); ok {
				*list, changed = items, true
			}
		}

		if changed {
			return &c
		}
	case *FromSandboxStmt:
		c := *v
		changed := derefArchive(&c.ArchiveExtraction, env)

		if id, ok := derefOne(v.ResourceId, env); ok {
			c.ResourceId, changed = id, true
		}

		if changed {
			return &c
		}
	case *SplitCodegenStmt:
		c := *v
		changed := false

		for _, list := range [...]*[]ANY{&c.Opts, &c.OutputIncludes} {
			if items, ok := derefList(*list, env); ok {
				*list, changed = items, true
			}
		}

		for _, item := range [...]*ANY{&c.ToolPath, &c.Prefix} {
			if value, ok := derefOne(*item, env); ok {
				*item, changed = value, true
			}
		}

		if changed {
			return &c
		}
	case *BaseCodegenStmt:
		c := *v
		changed := false

		for _, list := range [...]*[]ANY{&c.Opts, &c.OutputIncludes} {
			if items, ok := derefList(*list, env); ok {
				*list, changed = items, true
			}
		}

		for _, item := range [...]*ANY{&c.ToolPath, &c.Prefix} {
			if value, ok := derefOne(*item, env); ok {
				*item, changed = value, true
			}
		}

		if changed {
			return &c
		}
	}

	return s
}

func derefArchive(a *ArchiveExtraction, env Environment) bool {
	changed := false

	for _, list := range [...]*[]ANY{&a.OUTFiles, &a.OUTNoAutoFiles, &a.OutputIncludes, &a.InducedDeps, &a.Renames} {
		if items, ok := derefList(*list, env); ok {
			*list, changed = items, true
		}
	}

	if prefix, ok := derefOne(internAny(a.Prefix), env); ok {
		a.Prefix, changed = prefix.string(), true
	}

	return changed
}

func derefList(items []ANY, env Environment) ([]ANY, bool) {
	for _, item := range items {
		if strHasDollar(item.str().any()) {
			return expandRootDeferredTokens(items, env, tokenExpandLazy), true
		}
	}

	return items, false
}

func derefOne(item ANY, env Environment) (ANY, bool) {
	if item == 0 || !strHasDollar(item.str().any()) {
		return item, false
	}

	expanded := expandRootDeferredTokens([]ANY{item}, env, tokenExpandLazy)

	if len(expanded) != 1 {
		return internAny(joinAnys(expanded, " ")), true
	}

	return expanded[0], true
}

func derefPtr(item **ANY, env Environment) bool {
	if *item == nil {
		return false
	}

	if value, ok := derefOne(**item, env); ok {
		*item = &value

		return true
	}

	return false
}
