package main

type SourceFileGraph struct {
	children      DenseMap[VFS, []VFS]
	final         *IncludeScanner
	cythonMissing DenseMap[VFS, []IncludeDirective]
	cythonUses    []cythonSourceUse
}

func (s *IncludeScanner) sourceClosure(root VFS) Closure {
	graph := s.sourceFiles

	if graph.final == nil {
		final := newIncludeScannerWith(s.parsers, nil, s.onWarn, s.buckets)

		final.sourceFiles = graph
		final.sourceFactsOnly = true
		final.codegen = s.codegen

		graph.final = final
	}

	sc := ScanCtx{scanner: graph.final, tjc: tarjans.get()}

	defer tarjans.put(sc.tjc)

	return sc.closureOf(root)
}

func (ctx *GenCtx) finalizeSourceInputs() {
	for _, node := range ctx.emit.nodes.s {
		if node == nil || !node.sourceInputsPending {
			continue
		}

		var closures []Closure
		var extra []VFS
		var refs []NodeRef

		visit := func(view Closure) {
			if view.scanner == nil {
				return
			}

			seen := make(map[VFS]bool)

			var addRoot func(VFS)
			addRoot = func(parent VFS) {
				if seen[parent] {
					return
				}

				seen[parent] = true

				if parent.isSource() {
					closures = append(closures, view.scanner.sourceClosure(parent))

					return
				}

				children, _ := view.scanner.cachedChildren(parent)

				for _, child := range children {
					addRoot(child)
				}
			}

			if view.self != 0 {
				addRoot(view.self)
			}
		}

		visit(node.InputClosure)

		for _, view := range node.InputClosures {
			visit(view)
		}

		dedupers.with(func(seen *DeDuper) {
			for _, chunk := range node.Inputs {
				for _, input := range chunk {
					seen.addStable(input.strID())
				}
			}

			add := func(input VFS) {
				if seen.addStable(input.strID()) {
					extra = append(extra, input)
				}
			}

			for _, closure := range closures {
				closure.each(func(child VFS) {
					add(child)

					if child.isBuild() {
						if info := closure.scanner.codegen.lookup(child); info != nil {
							refs = append(refs, info.ProducerRef)

							if info.ProducerMainOut != 0 {
								add(info.ProducerMainOut)
							}
						}
					}
				})
			}
		})

		if len(extra) > 0 {
			node.Inputs = appendInputChunks(ctx.na, node.Inputs, ctx.na.inputList(ctx.na.vfsList(extra...)))
		}

		if len(refs) > 0 {
			node.DepRefs = ctx.na.uniqueRefs(node.DepRefs, refs)
		}

		node.InputClosures = nil
		node.InputClosure.scanner = nil
		node.sourceInputsPending = false
	}
}

type cythonSourceUse struct {
	root     VFS
	context  *ScanContext
	consumer NodeRef
}

func (ctx *GenCtx) resolveCythonSourceUses() bool {
	changed := ctx.resolveCythonScannerUses(ctx.scannerTarget)

	changed = ctx.resolveCythonScannerUses(ctx.scannerHost) || changed

	if ctx.scannerTargetPIC != nil {
		changed = ctx.resolveCythonScannerUses(ctx.scannerTargetPIC) || changed
	}

	return changed
}

func (ctx *GenCtx) resolveCythonScannerUses(s *IncludeScanner) bool {
	graph := s.sourceFiles
	changed := len(graph.cythonUses) > 0

	for i := 0; i < len(graph.cythonUses); i++ {
		use := graph.cythonUses[i]
		sc := s.getScanCtx(use.context, scanDomainCython, nil)
		seen := make(map[VFS]bool)

		var refs []NodeRef

		var visit func(VFS)
		visit = func(input VFS) {
			if seen[input] {
				return
			}

			seen[input] = true

			if input.isBuild() {
				if info := s.codegen.lookup(input); info != nil {
					refs = append(refs, info.ProducerRef)
				}
			}

			entry := sc.resolvedChildren(input)

			graph.children.put(input, entry.Paths)

			missing, _ := graph.cythonMissing.get(input)

			if len(missing) > 0 {
				var unresolved []IncludeDirective

				children := append([]VFS(nil), entry.Paths...)

				for _, include := range missing {
					mark := len(children)

					include.kind = includeSystem
					children = sc.resolve(children, input, 0, include)

					if len(children) == mark {
						unresolved = append(unresolved, include)
					}
				}

				graph.cythonMissing.put(input, unresolved)
				graph.children.put(input, children)
				entry.Paths = children
				s.scanCache.put1(input, entry)
			}

			for _, child := range entry.Paths {
				if isCythonLangFile(child.relString()) {
					visit(child)
				}
			}
		}
		visit(use.root)

		node := ctx.emit.nodes.s[use.consumer]

		node.DepRefs = ctx.na.uniqueRefs(node.DepRefs, refs)
		s.putScanCtx(sc)
	}

	graph.cythonUses = nil

	return changed
}

func (ctx *GenCtx) repairCppSourceInputs(node *Node) {
	view := node.InputClosure
	s := view.scanner
	owner := s.codegen.lookup(view.self)
	sc := s.getScanCtx(node.CppIncludeContext, scanDomainCC, nil)

	defer s.putScanCtx(sc)

	sc.retryMissing = true

	seen := make(map[VFS]bool)

	var refs []NodeRef

	var visit func(VFS)
	visit = func(input VFS) {
		if seen[input] {
			return
		}

		seen[input] = true

		if input.isBuild() {
			if info := s.codegen.lookup(input); info != nil {
				refs = append(refs, info.ProducerRef)
			}
		}

		entry := sc.resolvedChildren(input)
		parser := s.parsers.registry.registeredParserFor(input.relString())

		if parsed, ok := s.parsers.cache.parsed.get(input.rel()); ok {
			parser = parsed.parser
		}

		if _, cpp := parser.(CIncludeDirectiveParser); cpp && input.isSource() && entry.Diagnostics != nil {
			children := append([]VFS(nil), entry.Paths...)

			var remaining []IncludeDiagnostic

			for _, diagnostic := range entry.Diagnostics.Entries {
				if owner.providesSourceInclude(input, diagnostic.Directive) {
					remaining = append(remaining, diagnostic)

					continue
				}

				include := diagnostic.Directive

				include.kind = includeSystem
				sc.diagnostics = nil
				children = sc.resolve(children, input, 0, include)

				if len(sc.diagnostics) > 0 {
					remaining = append(remaining, diagnostic)
				}

				sc.diagnostics = nil
			}

			entry = s.putChildren(input, children, remaining)
		}

		s.sourceFiles.children.put(input, entry.Paths)

		if entry.Diagnostics != nil {
			for _, diagnostic := range entry.Diagnostics.Entries {
				if !owner.providesSourceInclude(input, diagnostic.Directive) {
					s.onWarn(diagnostic.Warning)
				}
			}
		}

		for _, child := range entry.Paths {
			visit(child)
		}
	}

	visit(view.self)
	node.DepRefs = ctx.na.uniqueRefs(node.DepRefs, refs)
}
