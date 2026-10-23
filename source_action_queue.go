package main

import "slices"

type sourceQueueAction struct {
	seq      int
	priority int
	order    int
	info     *SourceActionInfo
	inputs   []VFS
	producer NodeRef
	round    uint64
	done     bool
	known    bool
}

func (e *EmitContext) numberSourceAction(info *SourceActionInfo) {
	if info.order == 0 {
		e.sourceActionOrder++
		info.order = e.sourceActionOrder
	}
}

func (e *EmitContext) sourceCompileAction(priority, seq int, input VFS) *SourceActionInfo {
	info := &SourceActionInfo{Priority: priority, DeclSeq: seq, Inputs: []VFS{input}}

	e.numberSourceAction(info)
	e.sourceInlineActions = append(e.sourceInlineActions, info)

	return info
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

func sourceActionOutput(info *GeneratedFileInfo) VFS {
	if info.ProducerMainOut != 0 {
		return info.ProducerMainOut
	}

	return info.OutputPath
}

func (e *EmitContext) scheduleSourceActions() {
	assert(e.srcsClosed, "source action positions before source queue closed")

	if e.sourceRounds == nil {
		e.sourceRounds = make(map[int]uint64)
		e.sourcePositions = make(map[*SourceActionInfo]uint64)

		queue := make([]*sourceQueueAction, 0, len(e.sourceProducers)+len(e.sourceRoots))
		byProducer := make(map[NodeRef]*sourceQueueAction, len(e.sourceProducers))
		byOutput := make(map[VFS]*sourceQueueAction, len(e.sourceProducers))

		for ref, info := range e.sourceProducers {
			priority := info.SourceAction.Priority

			if priority == 0 {
				priority = stmtPrioDefault
			}

			a := &sourceQueueAction{seq: info.SourceAction.DeclSeq, priority: priority, order: info.SourceAction.order, info: info.SourceAction, inputs: info.SourceAction.Inputs, producer: ref}

			queue = append(queue, a)
			byProducer[ref] = a
			byOutput[sourceActionOutput(info)] = a
		}

		for seq, meta := range e.sourceRoots {
			queue = append(queue, &sourceQueueAction{seq: seq, priority: stmtPrioDefault, inputs: []VFS{e.sourceRootInput(meta)}})
		}

		for _, info := range e.sourceInlineActions {
			queue = append(queue, &sourceQueueAction{seq: info.DeclSeq, priority: info.Priority, order: info.order, info: info, inputs: info.Inputs})
		}

		slices.SortFunc(queue, func(a, b *sourceQueueAction) int {
			if a.priority != b.priority {
				return a.priority - b.priority
			}

			if a.seq != b.seq {
				return a.seq - b.seq
			}

			return a.order - b.order
		})

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

					if e.resourceAvailable[input] {
						continue
					}

					info := e.codegen.lookup(input)

					if info == nil {
						known = false

						continue
					}

					if info.OwnerModule != e.instance.Path {
						continue
					}

					producer := byProducer[info.ProducerRef]

					if producer == nil {
						producer = byOutput[sourceActionOutput(info)]
					}

					if producer == nil && info.OwnerSerial != 0 && info.OwnerSerial != e.frame.serial {
						continue
					}

					if producer == nil || producer == a || producer.seq == 0 && producer.info.Priority == 0 || producer.done && !producer.known {
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
					if a.info != nil {
						e.sourcePositions[a.info] = a.round
					} else {
						e.sourceRounds[a.seq] = a.round
					}
				}
			}
		}
	}
}

func (e *EmitContext) sourceActionRound(seq int) (uint64, bool) {
	e.scheduleSourceActions()

	round, ok := e.sourceRounds[seq]

	return round, ok
}

func (e *EmitContext) sourcePosition(info *SourceActionInfo) (uint64, bool) {
	e.scheduleSourceActions()

	round, ok := e.sourcePositions[info]

	return round, ok
}
