package main

import "slices"

type NodeInputs struct {
	files       InputChunks
	roots       InputChunks
	sourceRoots []VFS
	modulePeers []VFS
	text        []VFS
	scanner     *IncludeScanner
	na          *NodeArenas
	pending     *PendingEmit
	root        VFS
	deferred    bool
}

func (e *EmitContext) prepareNodeInputs(node *Node) {
	plan := &node.Inputs

	if plan.root != 0 {
		plan.roots = nil
	} else if plan.roots == nil {
		plan.roots = plan.files
	}

	plan.na = e.ctx.na
	plan.scanner = e.scanner
	plan.deferred = plan.deferred || len(plan.modulePeers) > 0

	e.checkDeclaredInputs(plan)

	plan.eachRoot(func(root VFS) {
		closure, cached := e.scanner.closure(root)

		plan.deferred = plan.deferred || !cached || closure.incomplete

		if info := e.codegen.lookup(root); info != nil && info.IncludedInputs != nil {
			plan.deferred = true
		}
	})

	for _, output := range node.Outputs {
		if info := e.codegen.lookup(output); info != nil && len(info.InputLoopSources) > 0 {
			plan.sourceRoots = node.Outputs
			plan.deferred = true

			break
		}
	}

	if !plan.deferred {
		plan.scanner = nil
	}
}

func (e *EmitContext) checkDeclaredInputs(plan *NodeInputs) {
	missing := func(v VFS) {
		if v.isSource() && !e.ctx.fs.isFilePathClean(v.relString()) {
			e.ctx.onWarn(Warn{Kind: WarnMissingSource, Message: "source file not found: " + v.string()})
		}
	}

	if plan.root != 0 {
		missing(plan.root)

		return
	}

	for _, chunk := range plan.roots {
		for _, v := range chunk {
			missing(v)
		}
	}
}

func (p *NodeInputs) eachRoot(visit func(VFS)) {
	if p.root != 0 {
		visit(p.root)
	}

	for _, chunk := range p.roots {
		for _, root := range chunk {
			if !root.isSource() || !slices.Contains(p.text, root) {
				visit(root)
			}
		}
	}
}

func (p *NodeInputs) eachModuleRoot(visit func(VFS)) {
	for _, peer := range p.modulePeers {
		if info := p.scanner.codegen.moduleInputs(peer); info != nil {
			for _, root := range info.IncludeRoots {
				visit(root)
			}
		}
	}
}

func (p *NodeInputs) evaluate() InputChunks {
	if p.scanner == nil || !p.deferred {
		return p.files
	}

	var closures []Closure
	var includedInputs InputChunks

	p.eachRoot(func(root VFS) {
		closures = append(closures, p.scanner.establishedClosure(root))

		if info := p.scanner.codegen.lookup(root); info != nil && info.IncludedInputs != nil {
			for _, included := range info.IncludedInputs.Roots {
				closure := p.scanner.establishedClosure(included)
				sources := closure.collect(p.na, VFS.isSource)

				includedInputs = append(includedInputs, sources)
			}
		}
	})

	for _, root := range p.sourceRoots {
		if info := p.scanner.codegen.lookup(root); info != nil {
			includedInputs = append(includedInputs, info.InputLoopSources)

			if info.IncludedInputs != nil {
				for _, parsed := range info.IncludedInputs.Roots {
					closure := p.scanner.establishedClosure(parsed)

					includedInputs = append(includedInputs, closure.collect(p.na, VFS.isSource))
				}
			}
		}
	}

	p.eachModuleRoot(func(root VFS) {
		closure := p.scanner.establishedClosure(root)

		for _, bucket := range closure.bucketList() {
			if len(bucket) > 0 && bucket[0].isBuild() {
				includedInputs = append(includedInputs, bucket)
			}
		}
	})

	var result InputChunks

	dedupers.with(func(seen *DeDuper) {
		bound := len(p.files) + len(includedInputs)

		for _, closure := range closures {
			bound += len(closure.bucketList())
		}

		chunks := p.na.inputs.alloc(bound)[:0]

		for _, chunk := range p.files {
			chunks = append(chunks, chunk)

			for _, input := range chunk {
				seen.addStable(input.strID())
			}
		}

		for _, bucket := range includedInputs {
			if extra := p.na.filterSeen(seen, bucket); len(extra) > 0 {
				chunks = append(chunks, extra)
			}
		}

		for _, closure := range closures {
			for _, bucket := range closure.bucketList() {
				if extra := p.na.filterSeen(seen, bucket); len(extra) > 0 {
					chunks = append(chunks, extra)
				}
			}
		}

		p.na.inputs.commit(len(chunks))
		result = chunks[:len(chunks):len(chunks)]
	})

	return result
}

func (p *NodeInputs) activate(seen *BitSet, selectProducer func(NodeRef), modulePeers bool) {
	var visit func(VFS)
	visit = func(file VFS) {
		if seen.has(uint32(file)) {
			return
		}

		entry, bound := p.scanner.codegen.files.get1(file)

		if bound && !entry.Deferred || file.isBuild() {
			seen.add(uint32(file))
		}

		if file.isBuild() {
			if info := p.scanner.codegen.use(file); info != nil {
				selectProducer(info.ProducerRef)
			}

			entry = p.scanner.bindGeneratedIncludes(file)
		}

		for _, child := range entry.Paths {
			visit(child)
		}
	}

	if modulePeers {
		p.eachModuleRoot(visit)
	} else {
		p.eachRoot(visit)
	}
}

func (n *Node) establishedInputRefs(na *NodeArenas) {
	plan := &n.Inputs

	if plan.scanner == nil || !plan.deferred {
		return
	}

	refs := nodeRefScratches.get()

	refs = append(refs, n.DepRefs...)

	addInput := func(input VFS) {
		if info := plan.scanner.codegen.lookup(input); info != nil && info.ProducerRef != n.Ref {
			refs = append(refs, info.ProducerRef)
		}
	}

	addBuildRefs := func(closure Closure) {
		for _, bucket := range closure.bucketList() {
			if len(bucket) == 0 || !bucket[0].isBuild() {
				continue
			}

			for _, input := range bucket {
				addInput(input)
			}
		}
	}

	plan.eachRoot(func(root VFS) {
		closure := plan.scanner.establishedClosure(root)

		addInput(closure.self)
		addBuildRefs(closure)
	})

	plan.eachModuleRoot(func(root VFS) {
		addBuildRefs(plan.scanner.establishedClosure(root))
	})

	n.DepRefs = na.uniqueRefs(refs)
	nodeRefScratches.put(refs)
}

func (n *Node) consumeInputs() InputChunks {
	inputs := n.Inputs.evaluate()

	n.Inputs = NodeInputs{}

	return inputs
}
