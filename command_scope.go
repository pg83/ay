package main

type CommandScope struct {
	tick               uint32
	scanner            *IncludeScanner
	headers            []VFS
	protoInclude       []VFS
	protoArgs          []ANY
	addIncl            []VFS
	peerAddInclGlobal  []VFS
	peerCFlagsGlobal   []ANY
	ownCFlagsGlobal    []ANY
	peerCXXFlagsGlobal []ANY
	ownCXXFlagsGlobal  []ANY
}

type ScopedCommand struct {
	scope    *CommandScope
	tick     uint32
	rendered scopedRenderer
}

type scopedRenderer interface {
	rebindScope(*CommandScope)
}

func (e *EmitContext) commandScope() *CommandScope {
	if e.scope != nil {
		return e.scope
	}

	d, na := e.d, e.ctx.na

	e.scope = &CommandScope{
		scanner:            e.scanner,
		protoInclude:       na.vfsList(concat(dedup(d.protoIncludeLocal), d.cc.ProtoIncludePeers)...),
		addIncl:            d.cc.AddIncl,
		peerAddInclGlobal:  d.cc.PeerAddInclGlobal,
		peerCFlagsGlobal:   na.anyList(d.cc.PeerCFlagsGlobal...),
		ownCFlagsGlobal:    na.anyList(d.cc.OwnCFlagsGlobal...),
		peerCXXFlagsGlobal: na.anyList(d.cc.PeerCXXFlagsGlobal...),
		ownCXXFlagsGlobal:  na.anyList(d.cc.OwnCXXFlagsGlobal...),
	}

	return e.scope
}

func (s *CommandScope) protoIncludeArgs(na *NodeArenas) []ANY {
	if s.protoArgs == nil {
		args := na.anys.alloc(len(s.protoInclude))[:0]

		for _, p := range s.protoInclude {
			args = append(args, internV("-I=", p.string()).any())
		}

		na.anys.commit(len(args))
		s.protoArgs = args[:len(args):len(args)]
	}

	return s.protoArgs
}

func (ctx *GenCtx) nextExportTick() uint32 {
	ctx.exportTick++

	return ctx.exportTick
}

func (ctx *GenCtx) visitScoped(sc *ScopedCommand) *CommandScope {
	if sc.scope == nil && ctx.frameDepth > 0 {
		frame := ctx.frames[ctx.frameDepth-1]

		if e := &frame.emitCtx; e.frame == frame && e.serial == frame.serial {
			sc.scope = e.commandScope()
			sc.tick = ctx.nextExportTick()
		}
	}

	return sc.scope
}

func (sc *ScopedCommand) claim(scope *CommandScope) {
	if sc.scope == scope || sc.scope != nil && sc.tick < scope.tick {
		return
	}

	sc.scope, sc.tick = scope, scope.tick

	if sc.rendered != nil {
		sc.rendered.rebindScope(scope)
	}
}

func (e *EmitContext) publishSearchScope(result *ModuleEmitResult) {
	if len(e.moduleIncludeRoots) == 0 || e.moduleIsFake(result) && e.instance.Kind != KindForeignDescent {
		return
	}

	na := e.ctx.na
	headers := na.vfs.alloc(len(e.moduleIncludeRoots))[:0]

	for _, root := range e.moduleIncludeRoots {
		if root.isSource() {
			headers = append(headers, root)
		}
	}

	na.vfs.commit(len(headers))

	if len(headers) == 0 {
		return
	}

	scope := e.commandScope()

	scope.headers = headers[:len(headers):len(headers)]
	scope.tick = e.ctx.nextExportTick()

	result.SearchScope = scope
	e.ctx.exportOrder = append(e.ctx.exportOrder, result)
}

func (ctx *GenCtx) bindCommandScopes() {
	seen := map[*IncludeScanner]*BitSet{}

	for _, result := range ctx.exportOrder {
		scope := result.SearchScope
		visited := seen[scope.scanner]

		if visited == nil {
			visited = &BitSet{}
			seen[scope.scanner] = visited
		}

		reg := scope.scanner.codegen

		var visit func(VFS)

		visit = func(file VFS) {
			if visited.has(uint32(file)) {
				return
			}

			visited.add(uint32(file))

			entry, _ := reg.files.get1(file)

			if file.isBuild() {
				if info := reg.lookup(file); info != nil && info.Scoped != nil {
					info.Scoped.claim(scope)
				}

				entry = scope.scanner.bindGeneratedIncludes(file)
			}

			for _, child := range entry.Paths {
				visit(child)
			}
		}

		for _, header := range scope.headers {
			visit(header)
		}
	}
}
