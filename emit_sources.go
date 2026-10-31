package main

import (
	"fmt"
	"strings"
)

func (e *EmitContext) emitOneSource(meta SrcMeta) {
	e.resolveSrc(&meta)
	meta.Resolved.Path = e.sourceForEmission(meta)
	meta.Resolved.Fixed = true

	src := e.cppStyleSourceResolved(meta.Source, meta.Resolved.Path)

	meta.Source = src

	if meta.PyMeta != 0 {
		i := meta.PyMeta - 1
		py := e.pyMetas[i]

		e.pyMetas[i] = PySourceMeta{}

		if py.Kind == pySourceProtoInput {
			e.emitPyProtoSource(src, e.sourceForEmission(meta), py.Group, py.Module)
		} else {
			ps := PySrc{Path: src.vfs(), Existing: meta.Resolved.Existing, PySourceMeta: py}

			e.collectPySource(ps.Path, py)

			if (!isProgramModuleType(e.d.moduleStmt.Name) || pyModuleTypeUsesPython3(e.d.moduleStmt.Name) || e.d.usePython2 || e.d.pythonSourceCallback) && (py.Kind == pySourceProto || !e.d.pyBuildNoPYC) {
				e.emitPyYapyc(ps, e.pyccTool())
			}

			if py.Kind == pySourceGenerated && !e.producersOnly() {
				e.packResources(ResourcePack{DontCompress: true,
					Tag: e.d.unit.HashTag, Items: e.pySrcResourceItems(e.appendPyResEntries(e.resEntries[:0], ps)),
					RawCompile: CompileSpec{ForceCxx: true, CFlags: resourceCxxFlags},
				})
			}
		}

		return
	}

	if e.producersOnly() {
		switch srcExtClassOf(src.relOrSelf().any()) {
		case srcExtCSource, srcExtRodata, srcExtCuda, srcExtYasm, srcExtAsm, srcExtGo, srcExtFortran:
			return
		}
	}

	switch srcExtClassOf(src.relOrSelf().any()) {
	case srcExtRos:
		e.emitRosSource(meta, nil)
	case srcExtHeader:
		if meta.Compile.FlatOutput {
			throwFmt("SRC(%s) produced no outputs", src.string())
		}

		header := e.sourceForEmission(meta)

		if header.isSource() {
			e.moduleIncludeRoots = append(e.moduleIncludeRoots, header)
			e.scanner.discoverInput(header, e.d.scanCtx)
		}

		return
	case srcExtCSource:
		srcVFS := src.vfs()

		if srcVFS == 0 {
			srcVFS = e.sourceForEmission(meta)
		}

		ref, out, accepted := e.emitCCWith(srcVFS, 0, e.ccInputsForSource(meta), meta.CompileRef)

		if accepted {
			e.collectObj(ref, out, meta)
		}
	case srcExtGettext:
		input := e.sourceForEmission(meta)

		e.emitGettextSource(input, e.gettextOutput(input))
	case srcExtFortran:
		e.emitFortranSource(meta)
	case srcExtRodata:
		e.emitLibraryRodataSource(meta, e.ccInputsForSource(meta))
	case srcExtAsm:
		if isGoModuleType(e.d.unit.Type) && strings.HasSuffix(src.string(), ".s") {
			e.collectGoSource(meta, true)
		} else {
			e.emitLibraryAsmSource(meta, e.ccInputsForSource(meta))
		}
	case srcExtYasm:
		e.emitLibraryYasmSource(meta, e.ccInputsForSource(meta))
	case srcExtCuda:
		e.emitLibraryCudaSource(meta, e.ccInputsForSource(meta))
	case srcExtGztProto:
		e.emitLibraryGztProtoCompile(src)

		if !isPythonProtoModule(e.d) {
			meta.Source = e.gztGenProto(src.string()).any()

			e.enqueueSrc(meta)
		}
	case srcExtProto:
		if isGoModuleType(e.d.unit.Type) {
			e.emitGoProtoSource(meta)
		} else if isPythonProtoModule(e.d) && !meta.Auto && !meta.Global {
			e.emitPyProtoSource(src, e.sourceForEmission(meta), meta.Stmt, 0)
			e.emitPyProtoGroupResources(meta.Stmt)
		} else {
			e.emitCppProtoFamilySource(meta, cppProtoSpec)
		}
	case srcExtFbs64:
		e.emitLibraryFlatcSource(meta, &flatcVariantFL64)
	case srcExtFbs:
		if e.instance.Language == LangFbsJavaNative {
			return
		}

		if isGoModuleType(e.d.unit.Type) {
			e.emitGoFlatcSource(meta)
		} else {
			e.emitLibraryFlatcSource(meta, &flatcVariantFL)
		}
	case srcExtGo:
		if isGoModuleType(e.d.unit.Type) {
			e.collectGoSource(meta, false)
		}
	case srcExtRl6:
		e.emitLibraryRagel6Source(meta)
	case srcExtY:
		e.emitBisonY(meta)
	case srcExtEv:
		if isGoModuleType(e.d.unit.Type) {
			e.emitGoEvSource(meta)
		} else if isPythonProtoModule(e.d) && !meta.Auto && !meta.Global {
			e.emitPyProtoSource(src, e.sourceForEmission(meta), meta.Stmt, 0)
			e.emitPyProtoGroupResources(meta.Stmt)
		} else {
			e.emitLibraryEvSource(meta)
		}
	case srcExtRl:
		e.emitLibraryRagel5Source(meta)
	case srcExtFml:
		e.emitLibraryFmlSource(meta)
	case srcExtSfdl:
		e.emitLibrarySfdlSource(meta)
	case srcExtAsp:
		e.emitLibraryAspSource(meta)
	case srcExtFlex:
		e.emitLibraryFlexSource(meta)
	case srcExtHIn:
		e.emitLibraryHInSource(meta)
	case srcExtCppIn, srcExtCIn:
		e.emitLibraryCInSource(meta)
	case srcExtSc:
		e.emitLibrarySCSource(meta)
	case srcExtGperf:
		e.emitLibraryGperfSource(meta)
	case srcExtCfgProto:
		e.emitLibraryCfgProtoSource(meta)
	case srcExtSwg:
		e.emitLibrarySwigSource(meta)
	case srcExtSsqls:
		e.emitSsqlsSource(meta)
	default:
		if e.collectObjectSource(meta) {
			return
		}

		if meta.Global && e.collectModuleGlobalSource(src) {
			return
		}

		if isGoModuleType(e.d.unit.Type) && src.string() == "CGO_EXPORT" {
			return
		}

		e.ctx.onWarn(Warn{
			Kind:    WarnBadSrc,
			Message: fmt.Sprintf("%s: can't build anything from %q", e.instance.Path.relString(), src.string()),
		})
	}
}

func (e *EmitContext) collectObjectSource(meta SrcMeta) bool {
	name := meta.Source.relOrSelf().string()

	if !strings.HasSuffix(name, ".o") && !strings.HasSuffix(name, ".obj") {
		return false
	}

	input := e.sourceForEmission(meta)
	info := e.codegen.lookup(input)

	if input.isBuild() && info == nil {
		return false
	}

	if e.producersOnly() {
		return true
	}

	var ref NodeRef

	if info != nil {
		ref = info.ProducerRef
	}

	e.collectObj(ref, input, meta)

	return true
}

func (na *NodeArenas) objectBuildRefs(refs []NodeRef, paths []VFS) []NodeRef {
	for i, input := range paths {
		if !input.isSource() {
			continue
		}

		out := na.noderefs.alloc(len(refs))[:0]

		out = append(out, refs[:i]...)

		for j := i + 1; j < len(paths); j++ {
			if !paths[j].isSource() {
				out = append(out, refs[j])
			}
		}

		na.noderefs.commit(len(out))

		return out[:len(out):len(out)]
	}

	return refs
}
