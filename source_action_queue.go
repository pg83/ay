package main

import "slices"

type sourceQueueAction struct {
	seq      int
	inputs   []VFS
	producer NodeRef
	round    uint64
	done     bool
	known    bool
}

func (e *EmitContext) sourceRootMeta(meta SrcMeta) SrcMeta {
	if meta.Prio != stmtPrioDefault || !meta.Compile.FlatOutput || meta.Seq == 0 || meta.SourceQueueRank != 0 {
		return meta
	}

	if e.sourceRoots == nil {
		e.sourceRoots = make(map[int]SrcMeta)
	}

	meta.SourceQueueRank = 1
	if parent, ok := e.sourceRoots[meta.Seq]; ok {
		// A synthesized auto-source (such as the converter's schema) retains
		// its source instruction, but is one queue wave after its parent.
		meta.SourceQueueRank = parent.SourceQueueRank + 1
	} else {
		e.sourceRoots[meta.Seq] = meta
	}

	return meta
}

func (e *EmitContext) sourceRootInput(meta SrcMeta) VFS {
	outputs, _ := e.srcPosition(meta)
	for _, output := range outputs {
		if info := e.codegen.lookup(output); info != nil {
			if info.PrimaryInput != 0 {
				return info.PrimaryInput
			}
			if info.SourcePath != 0 {
				return info.SourcePath
			}
		}
	}

	return e.moduleSourceVFS(meta.Source)
}

func (e *EmitContext) sourceActionRound(seq int) (uint64, bool) {
	if e.sourceRounds == nil {
		e.sourceRounds = make(map[int]uint64)
		queue := make([]*sourceQueueAction, 0, len(e.sourceProducers)+len(e.sourceRoots))
		byProducer := make(map[NodeRef]*sourceQueueAction, len(e.sourceProducers))

		for ref, info := range e.sourceProducers {
			a := &sourceQueueAction{seq: info.SourceAction.DeclSeq, inputs: info.SourceAction.Inputs, producer: ref}
			queue = append(queue, a)
			byProducer[ref] = a
		}
		for seq, meta := range e.sourceRoots {
			queue = append(queue, &sourceQueueAction{seq: seq, inputs: []VFS{e.sourceRootInput(meta)}})
		}
		slices.SortFunc(queue, func(a, b *sourceQueueAction) int { return a.seq - b.seq })

		// Native switches to LastTry after a complete pass without progress.
		// Remaining actions then have no proved readiness position: retain
		// their existing fallback and unresolved/cycle diagnostics.
		for pending, progress := queue, true; len(pending) > 0 && progress; {
			wave := pending
			pending, progress = nil, false
			for _, a := range wave {
				ready, known := true, true
				for _, input := range a.inputs {
					if input.isSource() {
						if !e.ctx.fs.isFile(srcRootRel, input.relString()) {
							known = false
						}
						continue
					}

					info := e.codegen.lookup(input)
					if info == nil {
						known = false
						continue
					}
					if info.OwnerModule != e.instance.Path || info.OwnerSerial != 0 && info.OwnerSerial != e.frame.serial {
						continue
					}

					producer := byProducer[info.ProducerRef]
					if producer == nil || producer == a || producer.seq == 0 || producer.done && !producer.known {
						// Do not invent a native declaration point for an unrecorded
						// producer, or change existing unresolved-input diagnostics.
						known = false
						continue
					}
					ready = ready && producer.done
				}

				if known && !ready {
					a.round++
					pending = append(pending, a)
					continue
				}
				progress = true
				a.done, a.known = true, known
				if known {
					e.sourceRounds[a.seq] = a.round
				}
			}
		}
	}

	round, ok := e.sourceRounds[seq]
	return round, ok
}
