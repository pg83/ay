package main

type NodeArenas struct {
	cmds     *BumpAllocator[Cmd]
	vfs      *BumpAllocator[VFS]
	strs     *BumpAllocator[STR]
	chunks   *BumpAllocator[[]STR]
	inputs   *BumpAllocator[[]VFS]
	noderefs *BumpAllocator[NodeRef]
	nodes    *BumpAllocator[Node]
}

func newNodeArenas() *NodeArenas {
	return &NodeArenas{
		cmds:     newBumpAllocator[Cmd](1 << 8),
		vfs:      newBumpAllocator[VFS](1 << 12),
		strs:     newBumpAllocator[STR](1 << 12),
		chunks:   newBumpAllocator[[]STR](1 << 10),
		inputs:   newBumpAllocator[[]VFS](1 << 10),
		noderefs: newBumpAllocator[NodeRef](1 << 12),
		nodes:    newBumpAllocator[Node](1 << 10),
	}
}

func (na *NodeArenas) cmdList(cs ...Cmd) []Cmd {
	return na.cmds.list(cs...)
}

func (na *NodeArenas) vfsList(vs ...VFS) []VFS {
	return na.vfs.list(vs...)
}

func (na *NodeArenas) strList(ss ...STR) []STR {
	return na.strs.list(ss...)
}

func (na *NodeArenas) chunkList(ch ...[]STR) ArgChunks {
	return ArgChunks(na.chunks.list(ch...))
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

func (na *NodeArenas) inclArgList(addIncl []VFS, memo InclArgMemo) []STR {
	block := na.strs.alloc(len(addIncl))

	for i, p := range addIncl {
		block[i] = memo.arg(p)
	}

	na.strs.commit(len(addIncl))

	return block[:len(addIncl):len(addIncl)]
}

func (na *NodeArenas) argStrList(groups ...[]ARG) []STR {
	n := 0

	for _, g := range groups {
		n += len(g)
	}

	block := na.strs.alloc(n)
	k := 0

	for _, g := range groups {
		for _, a := range g {
			block[k] = a.str()
			k++
		}
	}

	na.strs.commit(n)

	return block[:n:n]
}
