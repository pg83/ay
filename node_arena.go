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
		noderefs: newBumpAllocator[NodeRef](1 << 12),
		nodes:    newBumpAllocator[Node](1 << 10),
	}
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
