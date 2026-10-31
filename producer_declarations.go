package main

import (
	"cmp"
	"slices"
)

type producerDeclaration struct {
	kind  int
	index int
}

func (d *ModuleData) recordProducerDeclarations() {
	if d.moduleStmt == nil {
		return
	}

	counts := [prodKindCount]int{
		prodUsrvFile: len(d.usrvFiles), prodResource: len(d.resources), prodPyGroup: len(d.pySrcGroups), prodPyRegister: len(d.pyRegister),
		prodPyNoCheck: 1, prodYaConf: min(len(d.yaConfJSON), 1),
		prodSrc: len(d.srcs), prodRunJava: len(d.runJavaPrograms),
		prodCuda: len(d.cudaSrcs), prodImplib: len(d.implibs), prodCopyFile: len(d.copyFiles),
		prodConfigureFile: len(d.configureFiles), prodAntlrRun: len(d.antlrRuns), prodAntlr4Grammar: len(d.antlr4Grammars),
		prodDecimalMD5: len(d.decimalMD5), prodBuildMn: len(d.buildMns), prodSplitCodegen: len(d.splitCodegens),
		prodBaseCodegen: len(d.baseCodegens), prodRunProgram: len(d.runPrograms), prodRunPython: len(d.runPython),
		prodArchiveAsm: len(d.archiveAsm), prodEnum: len(d.enumSrcs), prodArchive: len(d.archives),
		prodCheckConfigH: len(d.checkConfigHeaders), prodCython: len(d.cythonCpp), prodSwigAll: min(len(d.swigC), 1),
		prodJoinSrcs: len(d.joinSrcs), prodSproto: len(d.ymapsSprotoSrcs), prodLlvmBc: len(d.llvmBc),
		prodGoCodegen: len(d.goCodegens), prodLlvmCompile: len(d.llvmCompile), prodLuaCompile: len(d.luaCompile),
		prodUserverProto: len(d.userverProtos), prodListProto: len(d.listProtos), prodFromArchive: len(d.fromArchives),
		prodPchFile: len(d.pchFiles), prodProto2Fbs: len(d.proto2Fbs), prodPireInline: len(d.pireInline),
		prodFromSandbox: len(d.fromSandboxes), prodBundle: len(d.bundles), prodRagel6: len(d.ragel6Builds),
	}

	if d.pyMain != nil {
		counts[prodPyMain] = 1
	}

	if d.docs != nil {
		counts[prodDocsCopy] = len(d.docs.copies)
	}

	if counts == d.producerCounts {
		return
	}

	start := len(d.producerDeclarations)
	n := 0

	for kind, count := range counts {
		n += max(count-d.producerCounts[kind], 0)
	}

	d.producerDeclarations = slices.Grow(d.producerDeclarations, n)

	for kind, count := range counts {
		for index := d.producerCounts[kind]; index < count; index++ {
			d.producerDeclarations = append(d.producerDeclarations, producerDeclaration{kind, index})
		}
	}

	if n > 1 {
		slices.SortStableFunc(d.producerDeclarations[start:], func(a, b producerDeclaration) int {
			return cmp.Compare(d.producerDeclarationSequence(a), d.producerDeclarationSequence(b))
		})
	}

	d.producerCounts = counts
}

func (d *ModuleData) producerDeclarationSequence(decl producerDeclaration) int {
	switch decl.kind {
	case prodPyNoCheck:
		return -1
	case prodUsrvFile:
		return d.usrvFiles[decl.index].DeclSeq
	case prodSrc:
		return d.srcs[decl.index].Seq
	case prodResource:
		return d.resources[decl.index].DeclSeq
	case prodPyGroup:
		return d.pySrcGroups[decl.index].DeclSeq
	case prodRunProgram:
		return d.runPrograms[decl.index].DeclSeq
	case prodGoCodegen:
		return d.goCodegens[decl.index].seq
	case prodRunPython:
		return d.runPython[decl.index].DeclSeq
	case prodRunJava:
		return d.runJavaPrograms[decl.index].DeclSeq
	case prodFromSandbox:
		return d.fromSandboxes[decl.index].DeclSeq
	case prodBundle:
		return d.bundles[decl.index].DeclSeq
	case prodCopyFile:
		return d.copyFiles[decl.index].DeclSeq
	case prodJoinSrcs:
		return d.joinSrcs[decl.index].Seq
	case prodArchiveAsm:
		return d.archiveAsm[decl.index].DeclSeq
	case prodEnum:
		return d.enumSrcs[decl.index].DeclSeq
	case prodCuda:
		return d.cudaSrcs[decl.index].DeclSeq
	case prodSplitCodegen:
		return d.splitCodegens[decl.index].DeclSeq
	case prodLlvmBc:
		return d.llvmBc[decl.index].DeclSeq
	case prodPyMain:
		return d.pyMainDeclSeq
	case prodRagel6:
		return d.ragel6Builds[decl.index].DeclSeq
	}

	return d.declSeq + 1
}

func (e *EmitContext) orderedProducerPositions(positions []ProducerPos) []ProducerPos {
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

	var counts, starts [prodKindCount]int

	for _, pos := range positions {
		counts[pos.kind] = max(counts[pos.kind], pos.index+1)
	}

	for _, decl := range e.d.producerDeclarations {
		counts[decl.kind] = max(counts[decl.kind], decl.index+1)
	}

	n := 0

	for kind, count := range counts {
		starts[kind] = n
		n += count
	}

	e.prodRanks = slices.Grow(e.prodRanks[:0], n)[:n]

	ranks := e.prodRanks

	for i, pos := range positions {
		ranks[starts[pos.kind]+pos.index] = len(e.d.producerDeclarations) + i
	}

	for i, decl := range e.d.producerDeclarations {
		ranks[starts[decl.kind]+decl.index] = i
	}

	e.prodOrder = slices.Grow(e.prodOrder[:0], len(positions))[:len(positions)]

	for i := range positions {
		e.prodOrder[i] = i
	}

	slices.SortStableFunc(e.prodOrder, func(ai, bi int) int {
		a, b := &positions[ai], &positions[bi]

		if order := cmp.Compare(e.producerPriority(*a), e.producerPriority(*b)); order != 0 {
			return order
		}

		return cmp.Compare(ranks[starts[a.kind]+a.index], ranks[starts[b.kind]+b.index])
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
