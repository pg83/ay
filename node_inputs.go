package main

var (
	inputChunkScratches = newSliceScratchPool[[]VFS](8)
	closureScratches    = newSliceScratchPool[Closure](8)
)

type InputSpec struct {
	roots          InputChunks
	text           []VFS
	root           VFS
	existingSource VFS
}

type NodeSpec struct {
	Node
	input InputSpec
}

func (e *EmitContext) checkDeclaredInputs(node *NodeSpec) {
	missing := func(v VFS) {
		if v != node.input.existingSource && v.isSource() && !e.ctx.fs.isFilePathClean(v.relString()) {
			e.ctx.onWarn(Warn{Kind: WarnMissingSource, Message: "source file not found: " + v.string()})
		}
	}

	if node.input.root != 0 {
		missing(node.input.root)

		return
	}

	roots := node.input.roots

	if roots == nil {
		roots = node.Inputs
	}

	for _, chunk := range roots {
		for _, v := range chunk {
			missing(v)
		}
	}
}

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

func (inputs InputChunks) checkRoots() {
	for _, chunk := range inputs {
		for _, input := range chunk {
			if input.isBuild() != chunk[0].isBuild() {
				throwFmt("mixed-root node input chunk: %s, %s", chunk[0].string(), input.string())
			}
		}
	}
}
