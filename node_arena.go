package main

type NodeArenas struct {
	warnings      *BumpAllocator[moduleWarnings]
	flagBlocks    *BumpAllocator[compilerFlagBlocks]
	compilerFlags *BumpAllocator[CompilerFlagValue]
	cmds          *BumpAllocator[Cmd]
	envs          *BumpAllocator[EnvVar]
	vfs           *BumpAllocator[VFS]
	strs          *BumpAllocator[STR]
	chunks        *BumpAllocator[[]ANY]
	anys          *BumpAllocator[ANY]
	inputs        *BumpAllocator[[]VFS]
	noderefs      *BumpAllocator[NodeRef]
	nodes         *BumpAllocator[Node]
	exts          *BumpAllocator[KVExt]
	geninfos      *BumpAllocator[GeneratedFileInfo]
	moduleInputs  *BumpAllocator[ModuleInputInfo]
	pending       *BumpAllocator[PendingEmit]
	protoPBC      *BumpAllocator[protoPBCommon]
	protoPB       *BumpAllocator[protoPBPending]
	pyPB          *BumpAllocator[pyPBPending]
	dirs          *BumpAllocator[IncludeDirective]
	scanctxs      *BumpAllocator[ScanContext]
}

func (na *NodeArenas) resetWindows() {
	na.warnings.open = false
	na.flagBlocks.open = false
	na.compilerFlags.open = false
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
	na.moduleInputs.open = false
	na.pending.open = false
	na.protoPBC.open = false
	na.protoPB.open = false
	na.pyPB.open = false
	na.dirs.open = false
	na.scanctxs.open = false
}

func (na *NodeArenas) markStrict() {
	na.warnings.markStrict()
	na.flagBlocks.markStrict()
	na.compilerFlags.markStrict()
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
	na.moduleInputs.markStrict()
	na.pending.markStrict()
	na.protoPBC.markStrict()
	na.protoPB.markStrict()
	na.pyPB.markStrict()
	na.dirs.markStrict()
	na.scanctxs.markStrict()
}

func newNodeArenas() *NodeArenas {
	return &NodeArenas{
		warnings:      newBumpAllocator[moduleWarnings](),
		flagBlocks:    newBumpAllocator[compilerFlagBlocks](),
		compilerFlags: newBumpAllocator[CompilerFlagValue](),
		cmds:          newBumpAllocator[Cmd](),
		envs:          newBumpAllocator[EnvVar](),
		vfs:           newBumpAllocator[VFS](),
		strs:          newBumpAllocator[STR](),
		chunks:        newBumpAllocator[[]ANY](),
		anys:          newBumpAllocator[ANY](),
		inputs:        newBumpAllocator[[]VFS](),
		exts:          newBumpAllocator[KVExt](),
		geninfos:      newBumpAllocator[GeneratedFileInfo](),
		moduleInputs:  newBumpAllocator[ModuleInputInfo](),
		pending:       newBumpAllocator[PendingEmit](),
		protoPBC:      newBumpAllocator[protoPBCommon](),
		protoPB:       newBumpAllocator[protoPBPending](),
		pyPB:          newBumpAllocator[pyPBPending](),
		dirs:          newBumpAllocator[IncludeDirective](),
		scanctxs:      newBumpAllocator[ScanContext](),
		noderefs:      newBumpAllocator[NodeRef](),
		nodes:         newBumpAllocator[Node](),
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

func (na *NodeArenas) uniqueRefs(lists ...[]NodeRef) []NodeRef {
	n := 0

	for _, list := range lists {
		n += len(list)
	}

	refs := na.noderefs.alloc(n)[:0]

	for _, list := range lists {
		refs = append(refs, list...)
	}

	refs = dedupInPlace(refs)
	na.noderefs.commit(len(refs))

	return refs[:len(refs):len(refs)]
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

func (na *NodeArenas) chunkList(ch ...[]ANY) ArgChunks {
	return ArgChunks(na.chunks.list(ch...))
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
