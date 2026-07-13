package main

type NodeArenas struct {
	cmds     *BumpAllocator[Cmd]
	envs     *BumpAllocator[EnvVar]
	vfs      *BumpAllocator[VFS]
	strs     *BumpAllocator[STR]
	chunks   *BumpAllocator[[]ANY]
	anys     *BumpAllocator[ANY]
	inputs   *BumpAllocator[[]VFS]
	noderefs *BumpAllocator[NodeRef]
	nodes    *BumpAllocator[Node]
	exts     *BumpAllocator[KVExt]
	geninfos *BumpAllocator[GeneratedFileInfo]
	pending  *BumpAllocator[PendingEmit]
	protoPBC *BumpAllocator[protoPBCommon]
	protoPB  *BumpAllocator[protoPBPending]
	pyPB     *BumpAllocator[pyPBPending]
	dirs     *BumpAllocator[IncludeDirective]
	scanctxs *BumpAllocator[ScanContext]
}

func (na *NodeArenas) resetWindows() {
	na.cmds.open = false
	na.envs.open = false
	na.vfs.open = false
	na.strs.open = false
	na.chunks.open = false
	na.anys.open = false
	na.inputs.open = false
	na.noderefs.open = false
	na.nodes.open = false
	na.exts.open = false
	na.geninfos.open = false
	na.pending.open = false
	na.protoPBC.open = false
	na.protoPB.open = false
	na.pyPB.open = false
	na.dirs.open = false
	na.scanctxs.open = false
}

func (na *NodeArenas) markStrict() {
	na.cmds.markStrict()
	na.envs.markStrict()
	na.vfs.markStrict()
	na.strs.markStrict()
	na.chunks.markStrict()
	na.anys.markStrict()
	na.inputs.markStrict()
	na.noderefs.markStrict()
	na.nodes.markStrict()
	na.exts.markStrict()
	na.geninfos.markStrict()
	na.pending.markStrict()
	na.protoPBC.markStrict()
	na.protoPB.markStrict()
	na.pyPB.markStrict()
	na.dirs.markStrict()
	na.scanctxs.markStrict()
}

func newNodeArenas() *NodeArenas {
	return &NodeArenas{
		cmds:     newBumpAllocator[Cmd](),
		envs:     newBumpAllocator[EnvVar](),
		vfs:      newBumpAllocator[VFS](),
		strs:     newBumpAllocator[STR](),
		chunks:   newBumpAllocator[[]ANY](),
		anys:     newBumpAllocator[ANY](),
		inputs:   newBumpAllocator[[]VFS](),
		exts:     newBumpAllocator[KVExt](),
		geninfos: newBumpAllocator[GeneratedFileInfo](),
		pending:  newBumpAllocator[PendingEmit](),
		protoPBC: newBumpAllocator[protoPBCommon](),
		protoPB:  newBumpAllocator[protoPBPending](),
		pyPB:     newBumpAllocator[pyPBPending](),
		dirs:     newBumpAllocator[IncludeDirective](),
		scanctxs: newBumpAllocator[ScanContext](),
		noderefs: newBumpAllocator[NodeRef](),
		nodes:    newBumpAllocator[Node](),
	}
}

func (na *NodeArenas) pendingEmit(fn func()) *PendingEmit {
	p := na.pending.one()

	*p = PendingEmit{fn: fn}

	return p
}

func (na *NodeArenas) pendingEmitter(emitter pendingEmitter) *PendingEmit {
	p := na.pending.one()

	*p = PendingEmit{emitter: emitter}

	return p
}

func newStrictNodeArenas() *NodeArenas {
	na := newNodeArenas()

	if ownershipOn {
		na.markStrict()
	}

	return na
}

func (na *NodeArenas) cmdList(cs ...Cmd) []Cmd {
	return na.cmds.list(cs...)
}

func (na *NodeArenas) envList(vs ...EnvVar) EnvVars {
	return EnvVars(na.envs.list(vs...))
}

func (na *NodeArenas) refList(refs ...NodeRef) []NodeRef {
	n := 0

	for _, r := range refs {
		if r != 0 {
			n++
		}
	}

	if n == 0 {
		return nil
	}

	block := na.noderefs.alloc(n)
	k := 0

	for _, r := range refs {
		if r != 0 {
			block[k] = r
			k++
		}
	}

	na.noderefs.commit(k)

	return block[:k:k]
}

func (na *NodeArenas) vfsList(vs ...VFS) []VFS {
	return na.vfs.list(vs...)
}

func (na *NodeArenas) scanContext() *ScanContext {
	return na.scanctxs.one()
}

func (na *NodeArenas) anyList(as ...ANY) []ANY {
	return na.anys.list(as...)
}

func (na *NodeArenas) anyConcat(parts ...[]ANY) []ANY {
	n := 0

	for _, p := range parts {
		n += len(p)
	}

	block := na.anys.alloc(n)
	k := 0

	for _, p := range parts {
		k += copy(block[k:], p)
	}

	na.anys.commit(n)

	return block[:n:n]
}

func (na *NodeArenas) inclAnyList(addIncl []VFS, memo InclArgMemo) []ANY {
	block := na.anys.alloc(len(addIncl))

	for i, p := range addIncl {
		block[i] = memo.arg(p).any()
	}

	na.anys.commit(len(addIncl))

	return block[:len(addIncl):len(addIncl)]
}

func (na *NodeArenas) chunkList(ch ...[]ANY) ArgChunks {
	return ArgChunks(na.chunks.list(ch...))
}

func (na *NodeArenas) anyChunk(ss []STR) []ANY {
	block := na.anys.alloc(len(ss))

	for i, s := range ss {
		block[i] = s.any()
	}

	na.anys.commit(len(ss))

	return block[:len(ss):len(ss)]
}

func (na *NodeArenas) anyChunkVFS(vs []VFS) []ANY {
	block := na.anys.alloc(len(vs))

	for i, v := range vs {
		block[i] = v.any()
	}

	na.anys.commit(len(vs))

	return block[:len(vs):len(vs)]
}

func (na *NodeArenas) anyChunkAny(as []ANY) []ANY {
	return na.anys.list(as...)
}

func (na *NodeArenas) inputList(first []VFS, rest ...[]VFS) InputChunks {
	n := 1 + len(rest)
	dst := na.inputs.alloc(n)

	dst[0] = first
	copy(dst[1:], rest)
	na.inputs.commit(n)

	return InputChunks(dst[:n:n])
}

func (na *NodeArenas) srcChunk(v VFS) []VFS {
	return na.vfsList(v)
}

func (na *NodeArenas) dirList(vs ...IncludeDirective) []IncludeDirective {
	return na.dirs.list(vs...)
}

func (na *NodeArenas) dedupClosure(extra []VFS, groups ...[][]VFS) []VFS {
	total := len(extra)

	for _, g := range groups {
		for _, b := range g {
			total += len(b)
		}
	}

	if total == 0 {
		return nil
	}

	var out []VFS

	dedupers.with(func(deduper *DeDuper) {
		out = na.vfs.alloc(total)[:0]

		for _, v := range extra {
			if deduper.addStable(v.strID()) {
				out = append(out, v)
			}
		}

		for _, g := range groups {
			for _, b := range g {
				for _, v := range b {
					if deduper.addStable(v.strID()) {
						out = append(out, v)
					}
				}
			}
		}

		na.vfs.commit(len(out))
	})

	return out[:len(out):len(out)]
}

func (na *NodeArenas) dedupClosureChunks(closures ...Closure) InputChunks {
	bound := 0

	for _, cl := range closures {
		bound += len(cl.bucketList())

		if cl.self != 0 {
			bound++
		}
	}

	var result InputChunks

	dedupers.with(func(deduper *DeDuper) {
		chunks := na.inputs.alloc(bound)[:0]

		for _, cl := range closures {
			if cl.self != 0 && deduper.addStable(cl.self.strID()) {
				chunks = append(chunks, na.vfsList(cl.self))
			}

			for _, bucket := range cl.bucketList() {
				bucket = na.filterSeen(deduper, bucket)

				if len(bucket) > 0 {
					chunks = append(chunks, bucket)
				}
			}
		}

		na.inputs.commit(len(chunks))
		result = InputChunks(chunks[:len(chunks):len(chunks)])
	})

	return result
}

func (na *NodeArenas) dedupSourceVFS(inputs []VFS, extra [][]VFS) []VFS {
	bound := len(inputs)

	for _, b := range extra {
		bound += len(b)
	}

	var out []VFS

	dedupers.with(func(deduper *DeDuper) {
		out = na.vfs.alloc(bound)[:0]

		keep := func(input VFS) {
			if !input.isSource() {
				return
			}

			if !deduper.addStable(input.strID()) {
				return
			}

			out = append(out, input)
		}

		for _, input := range inputs {
			keep(input)
		}

		for _, bucket := range extra {
			if bucket[0].isBuild() {
				continue
			}

			for _, input := range bucket {
				if deduper.addStable(input.strID()) {
					out = append(out, input)
				}
			}
		}
		na.vfs.commit(len(out))
	})

	return out[:len(out):len(out)]
}

func (na *NodeArenas) filterSeen(dd *DeDuper, list []VFS) []VFS {
	for i, v := range list {
		if dd.addStable(v.strID()) {
			continue
		}

		out := na.vfs.alloc(len(list) - 1)[:0]

		out = append(out, list[:i]...)

		for _, w := range list[i+1:] {
			if dd.addStable(w.strID()) {
				out = append(out, w)
			}
		}

		na.vfs.commit(len(out))

		return out[:len(out):len(out)]
	}

	return list
}
