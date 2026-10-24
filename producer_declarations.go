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
		prodArchiveAsm: min(len(d.archiveAsm), 1), prodEnum: len(d.enumSrcs), prodArchive: len(d.archives),
		prodCheckConfigH: len(d.checkConfigHeaders), prodCythonAll: min(len(d.cythonCpp), 1), prodSwigAll: min(len(d.swigC), 1),
		prodJoinSrcs: len(d.joinSrcs), prodSproto: len(d.ymapsSprotoSrcs), prodLlvmBc: len(d.llvmBc),
		prodGoCodegen: len(d.goCodegens), prodLlvmCompile: len(d.llvmCompile), prodLuaCompile: len(d.luaCompile),
		prodUserverProto: len(d.userverProtos), prodListProto: len(d.listProtos), prodFromArchive: len(d.fromArchives),
		prodPchFile: len(d.pchFiles), prodProto2Fbs: len(d.proto2Fbs), prodPireInline: len(d.pireInline),
		prodFromSandbox: len(d.fromSandboxes), prodBundle: len(d.bundles),
	}

	if d.pyMain != nil {
		counts[prodPyMain] = 1
	}

	start := len(d.producerDeclarations)

	for kind, count := range counts {
		for index := d.producerCounts[kind]; index < count; index++ {
			d.producerDeclarations = append(d.producerDeclarations, producerDeclaration{kind, index})
		}
	}

	slices.SortStableFunc(d.producerDeclarations[start:], func(a, b producerDeclaration) int {
		return cmp.Compare(d.producerDeclarationSequence(a), d.producerDeclarationSequence(b))
	})

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
	}

	return d.declSeq + 1
}

func (e *EmitContext) orderedProducerPositions(positions []ProducerPos) []ProducerPos {
	module := e.instance.Path.relString()

	for i, file := range e.d.usrvFiles {
		positions = append(positions, ProducerPos{kind: prodUsrvFile, index: i,
			outs: []VFS{copyFileOutputVFS(module, file.File)},
			ins:  []VFS{e.sourceInputForScheduling(file.Tar)},
		})
	}

	for i, stmt := range e.d.fromSandboxes {
		outputs := make([]VFS, 0, len(stmt.OUTFiles)+len(stmt.OUTNoAutoFiles))

		for _, name := range concat(stmt.OUTFiles, stmt.OUTNoAutoFiles) {
			outputs = append(outputs, copyFileOutputVFS(module, name.string()))
		}

		positions = append(positions, ProducerPos{kind: prodFromSandbox, index: i, outs: outputs})
	}

	for i, bundle := range e.d.bundles {
		positions = append(positions, ProducerPos{kind: prodBundle, index: i, outs: []VFS{copyFileOutputVFS(module, bundle.Name)}})
	}

	if e.deferCC && !e.producersOnly() {
		for lo := 0; e.d.unit.Tag != unitTagPy3Bin && lo < len(e.resources); {
			hi := lo + 1

			for hi < len(e.resources) && !e.resources[hi-1].EndsBatch {
				hi++
			}

			var inputs []VFS

			for _, entry := range e.resources[lo:hi] {
				name := entry.Path

				if name == "-" {
					name = entry.SrcPath

					if _, inner, _, ok := rootrelSplit(entry.Key); ok {
						name = inner
					}
				}

				if name != "" {
					inputs = append(inputs, e.sourceInputForScheduling(name))
				}
			}

			positions = append(positions, ProducerPos{kind: prodResource, index: lo, end: hi, ins: inputs})
			lo = hi
		}

		for i := range e.d.pyRegister {
			positions = append(positions, ProducerPos{kind: prodPyRegister, index: i})
		}

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

	if e.deferCC && (!e.producersOnly() || e.ctx.peerTests) {
		for gi, group := range e.d.pySrcGroups {
			var inputs []VFS

			for _, src := range group.Srcs {
				inputs = append(inputs, e.sourceInputForScheduling(src.string()))
			}

			positions = append(positions, ProducerPos{kind: prodPyGroup, index: gi, ins: inputs})
		}
	}

	ranks := make(map[producerDeclaration]int, len(positions))

	for i, pos := range positions {
		ranks[producerDeclaration{pos.kind, pos.index}] = len(e.d.producerDeclarations) + i
	}

	for i, decl := range e.d.producerDeclarations {
		ranks[decl] = i
	}

	slices.SortStableFunc(positions, func(a, b ProducerPos) int {
		if order := cmp.Compare(e.producerPriority(a), e.producerPriority(b)); order != 0 {
			return order
		}

		return cmp.Compare(ranks[producerDeclaration{a.kind, a.index}], ranks[producerDeclaration{b.kind, b.index}])
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
