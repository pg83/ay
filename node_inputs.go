package main

import "slices"

var (
	closureScratches    = newSliceScratchPool[Closure](8)
	inputChunkScratches = newSliceScratchPool[[]VFS](8)
)

func (na *NodeArenas) rootInputList(first []VFS, rest ...[]VFS) InputChunks {
	i := 1

	for i < len(first) && first[i].isBuild() == first[0].isBuild() {
		i++
	}

	if i >= len(first) {
		return na.inputList(first, rest...)
	}

	chunks := na.inputs.alloc(1 + len(first) - i + len(rest))[:0]

	chunks = append(chunks, first[:i:i])
	chunks = appendRootInputRuns(chunks, first[i:])
	chunks = append(chunks, rest...)
	na.inputs.commit(len(chunks))

	return chunks[:len(chunks):len(chunks)]
}

func (p *NodeInputs) checkRoots() {
	for _, chunk := range p.files {
		for _, input := range chunk {
			if input.isBuild() != chunk[0].isBuild() {
				throwFmt("mixed-root node input chunk: %s, %s", chunk[0].string(), input.string())
			}
		}
	}
}

func appendRootInputRuns(chunks InputChunks, files []VFS) InputChunks {
	start := 0

	for i := 1; i < len(files); i++ {
		if files[i].isBuild() != files[start].isBuild() {
			chunks = append(chunks, files[start:i:i])
			start = i
		}
	}

	if len(files) > 0 {
		chunks = append(chunks, files[start:len(files):len(files)])
	}

	return chunks
}

type NodeInputs struct {
	files          InputChunks
	roots          InputChunks
	sourceRoots    []VFS
	modulePeers    []VFS
	text           []VFS
	scanner        *IncludeScanner
	na             *NodeArenas
	pending        *PendingEmit
	root           VFS
	existingSource VFS
	deferred       bool
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

	prepare := func(root VFS) {
		if plan.deferred {
			return
		}

		closure, cached := e.scanner.closure(root)

		plan.deferred = plan.deferred || !cached || closure.incomplete

		if info := e.codegen.lookup(root); info != nil && info.IncludedInputs != nil {
			plan.deferred = true
		}
	}

	missing := func(v VFS) {
		if v != plan.existingSource && v.isSource() && !e.ctx.fs.isFilePathClean(v.relString()) {
			e.ctx.onWarn(Warn{Kind: WarnMissingSource, Message: "source file not found: " + v.string()})
		}
	}

	if plan.root != 0 {
		missing(plan.root)
		prepare(plan.root)
	} else {
		for _, chunk := range plan.roots {
			for _, root := range chunk {
				missing(root)

				if !root.isSource() || !slices.Contains(plan.text, root) {
					prepare(root)
				}
			}
		}
	}

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

	na := p.na
	closures := closureScratches.get()
	includedInputs := inputChunkScratches.get()

	p.eachRoot(func(root VFS) {
		closures = append(closures, p.scanner.establishedClosure(root))

		if info := p.scanner.codegen.lookup(root); info != nil && info.IncludedInputs != nil {
			for _, included := range info.IncludedInputs.Roots {
				closure := p.scanner.establishedClosure(included)
				sources := closure.collect(na, VFS.isSource)

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

					includedInputs = append(includedInputs, closure.collect(na, VFS.isSource))
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

		chunks := na.inputs.alloc(bound)[:0]

		for _, chunk := range p.files {
			chunks = append(chunks, chunk)

			for _, input := range chunk {
				seen.addStable(input.strID())
			}
		}

		for _, bucket := range includedInputs {
			if extra := na.filterSeen(seen, bucket); len(extra) > 0 {
				chunks = append(chunks, extra)
			}
		}

		for _, closure := range closures {
			for _, bucket := range closure.bucketList() {
				if extra := na.filterSeen(seen, bucket); len(extra) > 0 {
					chunks = append(chunks, extra)
				}
			}
		}

		na.inputs.commit(len(chunks))
		result = chunks[:len(chunks):len(chunks)]
	})

	clear(closures)
	closureScratches.put(closures)
	clear(includedInputs)
	inputChunkScratches.put(includedInputs)

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
