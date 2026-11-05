package main

import (
	"cmp"
	"math"
	"slices"
)

func (e *EmitContext) producerSeq(pos ProducerPos, cythonPlans []CythonStmtPlan) int {
	d := e.d
	i := pos.index

	switch pos.kind {
	case prodPyNoCheck:
		return -1
	case prodCuda:
		return d.cudaSrcs[i].DeclSeq
	case prodImplib:
		return d.implibs[i].DeclSeq
	case prodCopyFile:
		return d.copyFiles[i].DeclSeq
	case prodConfigureFile:
		return d.configureFiles[i].DeclSeq
	case prodAntlrRun:
		return d.antlrRuns[i].DeclSeq
	case prodAntlr4Grammar:
		return d.antlr4Grammars[i].DeclSeq
	case prodDecimalMD5:
		return d.decimalMD5[i].DeclSeq
	case prodBuildMn:
		return d.buildMns[i].Seq
	case prodSplitCodegen:
		return d.splitCodegens[i].DeclSeq
	case prodBaseCodegen:
		return d.baseCodegens[i].DeclSeq
	case prodRunProgram:
		return d.runPrograms[i].DeclSeq
	case prodRunPython:
		return d.runPython[i].DeclSeq
	case prodRunJava:
		return d.runJavaPrograms[i].DeclSeq
	case prodArchiveAsm:
		return d.archiveAsm[i].DeclSeq
	case prodSrc:
		return e.prodSrcs[i].Seq
	case prodEnum:
		return d.enumSrcs[i].DeclSeq
	case prodArchive:
		return d.archives[i].DeclSeq
	case prodCheckConfigH:
		return d.checkConfigHeaders[i].DeclSeq
	case prodCython:
		return cythonPlans[i].stmt.Seq
	case prodSwigAll:
		return d.swigC[0].DeclSeq
	case prodJoinSrcs:
		return d.joinSrcs[i].Seq
	case prodSproto:
		return d.ymapsSprotoSrcs[i].DeclSeq
	case prodLlvmBc:
		return d.llvmBc[i].DeclSeq
	case prodDocsCopy:
		return d.docs.copies[i].declSeq
	case prodGoCodegen:
		return d.goCodegens[i].seq
	case prodLlvmCompile:
		return d.llvmCompile[i].DeclSeq
	case prodLuaCompile:
		return d.luaCompile[i].DeclSeq
	case prodUserverProto:
		return d.userverProtos[i].DeclSeq
	case prodListProto:
		return d.listProtos[i].DeclSeq
	case prodFromArchive:
		return d.fromArchives[i].DeclSeq
	case prodPchFile:
		return d.pchFiles[i].DeclSeq
	case prodProto2Fbs:
		return d.proto2Fbs[i].Seq
	case prodPireInline:
		return d.pireInline[i].DeclSeq
	case prodFromSandbox:
		return d.fromSandboxes[i].DeclSeq
	case prodBundle:
		return d.bundles[i].DeclSeq
	case prodRagel6:
		return d.ragel6Builds[i].DeclSeq
	case prodResource:
		return e.resources[i].DeclSeq
	case prodPyGroup:
		return d.pySrcGroups[i].DeclSeq
	case prodPyRegister:
		return d.pyRegister[i].DeclSeq
	case prodPyMain:
		return d.pyMainDeclSeq
	case prodYaConf:
		return d.yaConfDeclSeq
	case prodUsrvFile:
		return d.usrvFiles[i].DeclSeq
	}

	return math.MaxInt
}

func (e *EmitContext) orderedProducerPositions(positions []ProducerPos, cythonPlans []CythonStmtPlan) []ProducerPos {
	module := e.instance.Path.relString()
	na := e.ctx.na

	for i, file := range e.d.usrvFiles {
		positions = append(positions, ProducerPos{kind: prodUsrvFile, index: i,
			outs: na.vfsList(copyFileOutputVFS(module, file.File)),
			ins:  na.vfsList(e.sourceInputForScheduling(file.Tar)),
		})
	}

	for i, stmt := range e.d.fromSandboxes {
		outputs := vfsScratches.get()

		for _, name := range stmt.OUTFiles {
			outputs = append(outputs, copyFileOutputVFS(module, name.string()))
		}

		for _, name := range stmt.OUTNoAutoFiles {
			outputs = append(outputs, copyFileOutputVFS(module, name.string()))
		}

		positions = append(positions, ProducerPos{kind: prodFromSandbox, index: i, outs: na.vfsList(outputs...)})
		vfsScratches.put(outputs)
	}

	for i, bundle := range e.d.bundles {
		positions = append(positions, ProducerPos{kind: prodBundle, index: i, outs: na.vfsList(copyFileOutputVFS(module, bundle.Name))})
	}

	if e.deferCC && !e.producersOnly() {
		for lo := 0; e.d.unit.Tag != unitTagPy3Bin && lo < len(e.resources); {
			hi := lo + 1

			for hi < len(e.resources) && !e.resources[hi-1].EndsBatch {
				hi++
			}

			inputs := vfsScratches.get()

			for ri := lo; ri < hi; ri++ {
				entry := &e.resources[ri]
				name := entry.Path

				if name == "-" {
					name = entry.SrcPath

					if _, inner, _, ok := rootrelSplit(entry.Key); ok {
						name = inner
					}
				}

				if name != "" {
					entry.Resolved = resolveSource(e.ctx, e.instance, name, e.d.srcDirs)
					inputs = append(inputs, e.schedulingSource(name, entry.Resolved))
				}
			}

			positions = append(positions, ProducerPos{kind: prodResource, index: lo, end: hi, ins: na.vfsList(inputs...)})
			vfsScratches.put(inputs)
			lo = hi
		}

		for i := range e.d.pyRegister {
			positions = append(positions, ProducerPos{kind: prodPyRegister, index: i})
		}

		if !isProgramModuleType(e.d.moduleStmt.Name) || e.d.unit.Tag != 0 || len(e.resources) > 0 || e.d.pythonSourceCallback {
			if len(e.d.noCheckImports) > 0 {
				positions = append(positions, ProducerPos{kind: prodPyNoCheck})
			}

			if e.d.pyMain != nil {
				positions = append(positions, ProducerPos{kind: prodPyMain})
			}

			if len(e.d.yaConfJSON) > 0 {
				positions = append(positions, ProducerPos{kind: prodYaConf})
			}
		}
	}

	if e.deferCC && (!e.producersOnly() || e.ctx.peerTests) {
		outputs, inputs := vfsScratches.get(), vfsScratches.get()

		for gi, group := range e.d.pySrcGroups {
			outputs, inputs = outputs[:0], inputs[:0]

			for si := range group.Srcs {
				decl := &group.Srcs[si]
				src := decl.Token

				decl.Resolved = e.resolvePythonDeclaration(src, group.SourceRoot)

				if extIsProto(src.relOrSelf().string()) || extIsEv(src.relOrSelf().string()) {
					outs, ins := e.srcPosition(SrcMeta{Source: src, Resolved: decl.Resolved})

					outputs = append(outputs, outs...)
					inputs = append(inputs, ins...)
				} else {
					inputs = append(inputs, e.schedulingSource(src.string(), decl.Resolved))
				}
			}

			positions = append(positions, ProducerPos{kind: prodPyGroup, index: gi, outs: na.vfsList(outputs...), ins: na.vfsList(inputs...)})
		}

		vfsScratches.put(outputs)
		vfsScratches.put(inputs)
	}

	e.prodSeqs = slices.Grow(e.prodSeqs[:0], len(positions))[:len(positions)]
	e.prodOrder = slices.Grow(e.prodOrder[:0], len(positions))[:len(positions)]

	for i := range positions {
		e.prodSeqs[i] = e.producerSeq(positions[i], cythonPlans)
		e.prodOrder[i] = i
	}

	seqs := e.prodSeqs

	slices.SortStableFunc(e.prodOrder, func(ai, bi int) int {
		if order := cmp.Compare(e.producerPriority(positions[ai]), e.producerPriority(positions[bi])); order != 0 {
			return order
		}

		return cmp.Compare(seqs[ai], seqs[bi])
	})

	return positions
}

func (e *EmitContext) producerPriority(pos ProducerPos) int {
	if pos.kind == prodSrc {
		return e.prodSrcs[pos.index].Prio
	}

	if pos.kind == prodPyGroup {
		return e.d.pySrcGroups[pos.index].Prio
	}

	return stmtPrioDefault
}
