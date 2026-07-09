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
	kvs      *BumpAllocator[KV]
	geninfos *BumpAllocator[GeneratedFileInfo]
	dirs     *BumpAllocator[IncludeDirective]
	compiles *BumpAllocator[CompileSpec]
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
	na.kvs.open = false
	na.geninfos.open = false
	na.dirs.open = false
	na.compiles.open = false
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
	na.kvs.markStrict()
	na.geninfos.markStrict()
	na.dirs.markStrict()
	na.compiles.markStrict()
}

func newNodeArenas() *NodeArenas {
	return &NodeArenas{
		cmds:     newBumpAllocator[Cmd](1 << 8),
		envs:     newBumpAllocator[EnvVar](1 << 8),
		vfs:      newBumpAllocator[VFS](1 << 12),
		strs:     newBumpAllocator[STR](1 << 12),
		chunks:   newBumpAllocator[[]ANY](1 << 10),
		anys:     newBumpAllocator[ANY](1 << 12),
		inputs:   newBumpAllocator[[]VFS](1 << 10),
		exts:     newBumpAllocator[KVExt](1 << 8),
		kvs:      newBumpAllocator[KV](1 << 8),
		geninfos: newBumpAllocator[GeneratedFileInfo](1 << 10),
		dirs:     newBumpAllocator[IncludeDirective](1 << 10),
		compiles: newBumpAllocator[CompileSpec](1 << 8),
		noderefs: newBumpAllocator[NodeRef](1 << 12),
		nodes:    newBumpAllocator[Node](1 << 10),
	}
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

func (na *NodeArenas) compileSpec(c CompileSpec) *CompileSpec {
	p := na.compiles.one()

	*p = c

	return p
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

	deduper := dedupers.get()

	defer dedupers.put(deduper)

	out := na.vfs.alloc(total)[:0]

	for _, v := range extra {
		if deduper.add(v.strID()) {
			out = append(out, v)
		}
	}

	for _, g := range groups {
		for _, b := range g {
			for _, v := range b {
				if deduper.add(v.strID()) {
					out = append(out, v)
				}
			}
		}
	}

	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

func (na *NodeArenas) dedupSourceVFS(inputs []VFS, extra [][]VFS) []VFS {
	bound := len(inputs)

	for _, b := range extra {
		bound += len(b)
	}

	out := na.vfs.alloc(bound)[:0]
	deduper := dedupers.get()

	defer dedupers.put(deduper)

	keep := func(input VFS) {
		if !input.isSource() {
			return
		}

		if !deduper.add(input.strID()) {
			return
		}

		out = append(out, input)
	}

	for _, input := range inputs {
		keep(input)
	}

	eachBucketVFS(extra, keep)
	na.vfs.commit(len(out))

	return out[:len(out):len(out)]
}

func (na *NodeArenas) filterSeen(dd *DeDuper, list []VFS) []VFS {
	for i, v := range list {
		if dd.add(v.strID()) {
			continue
		}

		out := na.vfs.alloc(len(list) - 1)[:0]

		out = append(out, list[:i]...)

		for _, w := range list[i+1:] {
			if dd.add(w.strID()) {
				out = append(out, w)
			}
		}

		na.vfs.commit(len(out))

		return out[:len(out):len(out)]
	}

	return list
}
