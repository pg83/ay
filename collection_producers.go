package main

import (
	"cmp"
	"slices"
)

type producerDeclaration struct {
	kind  int
	index int
}

func (d *ModuleData) recordCollectionDeclarations() {
	if d.moduleStmt == nil || !isUnionModule(d.moduleStmt.Name) && d.moduleStmt.Name != tokPackageFinal {
		return
	}

	counts := [prodKindCount]int{
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

	for kind, count := range counts {
		for index := d.collectionProducerCounts[kind]; index < count; index++ {
			d.collectionDeclarations = append(d.collectionDeclarations, producerDeclaration{kind, index})
		}
	}

	d.collectionProducerCounts = counts
}

func (e *EmitContext) collectionProducerPositions(positions []ProducerPos) []ProducerPos {
	module := e.instance.Path.relString()

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

	ranks := make(map[producerDeclaration]int, len(positions))

	for i, pos := range positions {
		ranks[producerDeclaration{pos.kind, pos.index}] = len(e.d.collectionDeclarations) + i
	}

	for i, decl := range e.d.collectionDeclarations {
		ranks[decl] = i
	}

	slices.SortStableFunc(positions, func(a, b ProducerPos) int {
		return cmp.Compare(ranks[producerDeclaration{a.kind, a.index}], ranks[producerDeclaration{b.kind, b.index}])
	})

	return positions
}

func (e *EmitContext) scheduleCollectionProducers(positions []ProducerPos) []int {
	m := &e.ctx.prodOuts

	m.reset(vfsBound())

	for i, pos := range positions {
		for _, output := range pos.outs {
			m.put(output, int32(i))
		}
	}

	done := make([]bool, len(positions))
	order := e.prodOrder[:0]
	cursor, blocked := 0, 0

	for len(order) < len(positions) {
		i := cursor

		cursor = (cursor + 1) % len(positions)

		if done[i] {
			continue
		}

		ready := true

		for _, input := range positions[i].ins {
			if e.codegen.lookup(input) != nil {
				continue
			}

			from, declared := m.get(input)

			if declared && int(from) != i && !done[from] {
				ready = false

				break
			}
		}

		if !ready {
			blocked++

			if blocked == len(positions)-len(order) {
				throwFmt("gen: %s declares a dependency cycle among generating macros", e.instance.Path.relString())
			}

			continue
		}

		done[i] = true
		blocked = 0
		order = append(order, i)
	}

	return order
}
