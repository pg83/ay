package main

var (
	// Node-construction arenas: the small per-node slices every emitter builds —
	// the cmd list, the outputs, the arg-chunk and input-chunk headers, the token
	// blocks — land in shared bump arenas instead of one heap object each.
	// Committed blocks are never rewritten and chunk backing arrays never move, so
	// the slices are as good as heap ones for every consumer (uid, json writer,
	// executor). Same single-writer contract as the intern table: only the gen
	// goroutine constructs nodes; the executor reads already-committed blocks.
	nodeCmdArena   = newBumpAllocator[Cmd](1 << 8)
	nodeVFSArena   = newBumpAllocator[VFS](1 << 12)
	nodeSTRArena   = newBumpAllocator[STR](1 << 12)
	nodeChunkArena = newBumpAllocator[[]STR](1 << 10)
	nodeInputArena = newBumpAllocator[[]VFS](1 << 10)
)

// arenaList copies vs into a and returns the committed block. The variadic
// slice at the call site stays on the stack (it is only copied from), so a
// call replaces the heap literal it wraps.
func arenaList[T any](a *BumpAllocator[T], vs ...T) []T {
	n := len(vs)
	block := a.alloc(n)
	copy(block, vs)
	a.commit(n)

	return block[:n:n]
}

func cmdList(cs ...Cmd) []Cmd {
	return arenaList(nodeCmdArena, cs...)
}

func vfsList(vs ...VFS) []VFS {
	return arenaList(nodeVFSArena, vs...)
}

func strList(ss ...STR) []STR {
	return arenaList(nodeSTRArena, ss...)
}

func chunkList(ch ...[]STR) ArgChunks {
	return ArgChunks(arenaList(nodeChunkArena, ch...))
}

func inputList(ch ...[]VFS) InputChunks {
	return InputChunks(arenaList(nodeInputArena, ch...))
}

// argStrList is appendArgStr's arena twin: the converted []STR chunk lands in
// the node STR arena instead of a fresh heap slice.
func argStrList(groups ...[]ARG) []STR {
	n := 0

	for _, g := range groups {
		n += len(g)
	}

	block := nodeSTRArena.alloc(n)
	k := 0

	for _, g := range groups {
		for _, a := range g {
			block[k] = a.str()
			k++
		}
	}

	nodeSTRArena.commit(n)

	return block[:n:n]
}
