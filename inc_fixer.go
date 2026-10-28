package main

import "slices"

const (
	fixIncludes includeFixKind = iota
	fixPeerIncludes
	fixIncludedSources
	fixInputLoop
	fixModuleRoots
	fixProducer
)

type FixID uint32

type includeFixKind uint8

type includeFix struct {
	scanner *IncludeScanner
	file    VFS
	kind    includeFixKind
	pending *PendingEmit
	inputs  []VFS
	refs    []NodeRef
}

type IncludeFixer struct {
	entries Vec[includeFix]
	ids     *BumpAllocator[FixID]
}

func newIncludeFixer() *IncludeFixer {
	ids := newBumpAllocator[FixID]()

	ids.markStrict()

	return &IncludeFixer{ids: ids}
}

func (s *IncludeScanner) includeFix(file VFS, kind includeFixKind) FixID {
	f := s.fixer

	for i := range f.entries.s {
		entry := &f.entries.s[i]

		if entry.scanner == s && entry.file == file && entry.kind == kind {
			return FixID(i + 1)
		}
	}

	f.entries.pushBack(includeFix{scanner: s, file: file, kind: kind})

	return FixID(f.entries.len())
}

func (s *IncludeScanner) addInputFix(node *Node, file VFS, kind includeFixKind) FixID {
	id := s.includeFix(file, kind)

	if !slices.Contains(node.InputFixes, id) {
		n := len(node.InputFixes) + 1
		block := s.fixer.ids.alloc(n)[:n:n]

		copy(block, node.InputFixes)
		block[len(node.InputFixes)] = id
		s.fixer.ids.commit(len(block))
		node.InputFixes = block[:len(block):len(block)]
	}

	return id
}

func (s *IncludeScanner) deferClosure(node *Node, cl Closure, kind includeFixKind) {
	if cl.incomplete {
		s.addInputFix(node, cl.self, kind)
	}
}

func (s *IncludeScanner) activateInclude(file VFS, selectProducer func(NodeRef)) {
	if s.fixVisited.has(uint32(file)) {
		return
	}

	entry, bound := s.codegen.files.get1(file)

	if bound && !entry.Deferred || file.isBuild() {
		s.fixVisited.add(uint32(file))
	}

	if file.isBuild() {
		if info := s.codegen.use(file); info != nil {
			selectProducer(info.ProducerRef)
		}

		entry = s.bindGeneratedIncludes(file)
	}

	for _, child := range entry.Paths {
		s.activateInclude(child, selectProducer)
	}
}

func (f *IncludeFixer) activate(id FixID, selectProducer func(NodeRef)) {
	entry := f.entries.s[id-1]

	if entry.pending != nil {
		f.entries.s[id-1].pending = nil
		entry.pending.fire()
	}

	s := entry.scanner

	switch entry.kind {
	case fixIncludedSources, fixInputLoop:

	case fixModuleRoots:
		if info := s.codegen.moduleInputs(entry.file); info != nil {
			for _, root := range info.IncludeRoots {
				s.activateInclude(root, selectProducer)
			}
		}
	default:
		s.activateInclude(entry.file, selectProducer)
	}
}

func (f *IncludeFixer) resolve(entry *includeFix, na *NodeArenas) {
	s := entry.scanner
	inputs := vfsScratches.get()
	refs := nodeRefScratches.get()

	add := func(file VFS) {
		inputs = append(inputs, file)

		if file.isBuild() {
			if info := s.codegen.lookup(file); info != nil {
				refs = append(refs, info.ProducerRef)
			}
		}
	}

	closure := func(root VFS, buildsOnly bool) {
		cl := s.establishedClosure(root)

		if !buildsOnly && root.isBuild() {
			if info := s.codegen.lookup(root); info != nil {
				refs = append(refs, info.ProducerRef)
			}
		}

		for _, bucket := range cl.bucketList() {
			if len(bucket) > 0 && (!buildsOnly || bucket[0].isBuild()) {
				for _, file := range bucket {
					add(file)
				}
			}
		}
	}

	switch entry.kind {
	case fixIncludes, fixPeerIncludes:
		closure(entry.file, entry.kind == fixPeerIncludes)
	case fixIncludedSources, fixInputLoop:
		if info := s.codegen.lookup(entry.file); info != nil {
			if entry.kind == fixInputLoop {
				inputs = append(inputs, info.InputLoopSources...)
			}

			if info.IncludedInputs != nil {
				for _, root := range info.IncludedInputs.Roots {
					s.establishedClosure(root).each(func(file VFS) {
						if file.isSource() {
							inputs = append(inputs, file)
						}
					})
				}
			}
		}
	case fixModuleRoots:
		if info := s.codegen.moduleInputs(entry.file); info != nil {
			for _, root := range info.IncludeRoots {
				closure(root, true)
			}
		}
	}

	entry.inputs = na.vfsList(inputs...)
	entry.refs = na.uniqueRefs(refs)
	vfsScratches.put(inputs)
	nodeRefScratches.put(refs)
}

func (f *IncludeFixer) finish(e *StreamingEmitter) {
	for i := range f.entries.s {
		f.resolve(&f.entries.s[i], e.na)
	}

	for _, node := range e.nodes.s {
		if node == nil || len(node.InputFixes) == 0 || !e.exportEligible.has(uint32(node.Ref)) {
			continue
		}

		n, nr := 0, len(node.DepRefs)

		for _, id := range node.InputFixes {
			entry := &f.entries.s[id-1]

			n += len(entry.inputs)
			nr += len(entry.refs)
		}

		if n > 0 {
			extra := e.na.vfs.alloc(n)
			k := 0

			for _, id := range node.InputFixes {
				k += copy(extra[k:], f.entries.s[id-1].inputs)
			}

			e.na.vfs.commit(k)
			node.Inputs = appendInputChunks(e.na, node.Inputs, e.na.inputList(extra[:k:k]))
		}

		if nr > len(node.DepRefs) {
			refs := nodeRefScratches.get()

			refs = append(refs, node.DepRefs...)

			for _, id := range node.InputFixes {
				for _, ref := range f.entries.s[id-1].refs {
					if ref != node.Ref {
						refs = append(refs, ref)
					}
				}
			}

			node.DepRefs = e.na.uniqueRefs(refs)
			nodeRefScratches.put(refs)
		}
	}
}
