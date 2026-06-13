package main

// NodeArenas holds the bump arenas backing the small per-node slices every
// emitter builds — the cmd list, the outputs, the arg-chunk and input-chunk
// headers, the token blocks — so they land in shared chunks instead of one
// heap object each. Committed blocks are never rewritten and chunk backing
// arrays never move, so the slices are as good as heap ones for every
// consumer (uid, json writer, executor). Single-writer: only the gen
// goroutine constructs nodes; the executor reads already-committed blocks.
// One instance per gen run, owned by the emitter and shared via GenCtx.
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

// srcChunk wraps a single VFS as an input chunk — the [1]{src} head of a CC
// node's chunked inputs.
func (na *NodeArenas) srcChunk(v VFS) []VFS {
	return na.vfsList(v)
}

// argStrList is appendArgStr's arena twin: the converted []STR chunk lands in
// the node STR arena instead of a fresh heap slice.
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
