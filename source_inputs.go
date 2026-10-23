package main

type SourceFileGraph struct {
	children DenseMap[VFS, []VFS]
	final    *IncludeScanner
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
