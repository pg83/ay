package main

const (
	exportVisit ExportStep = iota
	exportUse
	exportPublish
)

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

type ExportStep uint8

type ExportEntry struct {
	module   *ModuleEmitResult
	scoped   *ScopedCommand
	end      int32
	step     ExportStep
	replayed bool
}

func (ctx *GenCtx) pushExport(entry ExportEntry) int32 {
	at := ctx.exportLen

	*ctx.exportLog.at(uint32(at)) = entry
	ctx.exportLen++

	return at
}

func (ctx *GenCtx) enterExport() int32 {
	return ctx.pushExport(ExportEntry{})
}

func (ctx *GenCtx) leaveExport(at int32, result *ModuleEmitResult) {
	end := ctx.exportLen

	*ctx.exportLog.at(uint32(at)) = ExportEntry{module: result, end: end}

	if result != nil && result.exportAt == 0 && end > at+1 {
		result.exportAt = at + 1
	}
}

func (ctx *GenCtx) logExport(entry ExportEntry) {
	entry.end = ctx.exportLen + 1
	ctx.pushExport(entry)
}

func (ctx *GenCtx) markExportRoot(start int32) {
	ctx.exportRoots = append(ctx.exportRoots, [2]int32{start, ctx.exportLen})
}

func (ctx *GenCtx) replayExport() {
	log := &ctx.exportLog
	tick := uint32(0)

	var replay func(at int32)

	replay = func(at int32) {
		entry := log.at(uint32(at))

		if entry.replayed {
			return
		}

		entry.replayed = true

		switch entry.step {
		case exportUse:
			tick++
			entry.scoped.tick = tick
		case exportPublish:
			tick++
			entry.module.SearchScope.tick = tick
			ctx.exportOrder = append(ctx.exportOrder, entry.module)
		case exportVisit:
			if entry.end > at+1 {
				for next := at + 1; next < entry.end; next = log.at(uint32(next)).end {
					replay(next)
				}
			} else if module := entry.module; module != nil && module.exportAt != 0 {
				replay(module.exportAt - 1)
			}
		}
	}

	for _, root := range ctx.exportRoots {
		for at := root[0]; at < root[1]; at = log.at(uint32(at)).end {
			replay(at)
		}
	}

	for at := int32(0); at < ctx.exportLen; at = log.at(uint32(at)).end {
		replay(at)
	}
}

func (ctx *GenCtx) visitScoped(sc *ScopedCommand) *CommandScope {
	if sc.scope == nil && ctx.frameDepth > 0 {
		frame := ctx.frames[ctx.frameDepth-1]

		if e := &frame.emitCtx; e.frame == frame && e.serial == frame.serial {
			sc.scope = e.commandScope()
			ctx.logExport(ExportEntry{scoped: sc, step: exportUse})
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
	result.SearchScope = scope

	e.ctx.logExport(ExportEntry{module: result, step: exportPublish})
}

func (ctx *GenCtx) bindCommandScopes() {
	ctx.replayExport()

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
				if info := reg.lookup(file); info != nil && info.scoped() != nil {
					info.scoped().claim(scope)
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
