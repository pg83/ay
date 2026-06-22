package main

type NodeArenas struct {
	cmds   *BumpAllocator[Cmd]
	vfs    *BumpAllocator[VFS]
	strs   *BumpAllocator[STR]
	chunks *BumpAllocator[[]STR]
	inputs *BumpAllocator[[]VFS]
}

func newNodeArenas() *NodeArenas {
	return &NodeArenas{
		cmds:   newBumpAllocator[Cmd](1 << 8),
		vfs:    newBumpAllocator[VFS](1 << 12),
		strs:   newBumpAllocator[STR](1 << 12),
		chunks: newBumpAllocator[[]STR](1 << 10),
		inputs: newBumpAllocator[[]VFS](1 << 10),
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

func (na *NodeArenas) inputList(ch ...[]VFS) InputChunks {
	return InputChunks(na.inputs.list(ch...))
}

func (na *NodeArenas) srcChunk(v VFS) []VFS {
	return na.vfsList(v)
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
