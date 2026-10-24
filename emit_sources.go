package main

import (
	"fmt"
	"strings"
)

func (e *EmitContext) emitOneSource(meta SrcMeta) {
	src := e.cppStyleSource(meta.Source)

	meta.Source = src

	if meta.PyMeta != 0 {
		i := meta.PyMeta - 1
		py := e.pyMetas[i]

		e.pyMetas[i] = PySourceMeta{}

		if py.Kind == pySourceProtoInput {
			group := e.d.pySrcGroups[py.Group]

			e.emitPyProtoSource(src, py.Group, py.Module, group.Prio, group.DeclSeq)
		} else {
			e.collectPySource(src.vfs(), py)
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
		header := e.moduleSourceVFS(src)

		if header.isSource() {
			e.moduleIncludeRoots = append(e.moduleIncludeRoots, header)
		}

		return
	case srcExtCSource:
		srcVFS := src.vfs()

		if srcVFS == 0 {
			srcVFS = e.moduleSourceVFS(src)
		}

		ref, out := e.emitCCWith(srcVFS, e.ccInputsFor(meta.Compile), meta.CompileRef)

		if meta.CompileRef == 0 {
			e.collectObj(ref, out, meta)
		}
	case srcExtGettext:
		input := e.moduleSourceVFS(src)

		e.emitGettextSource(input, e.gettextOutput(input))
	case srcExtFortran:
		e.emitFortranSource(meta)
	case srcExtRodata:
		e.emitLibraryRodataSource(meta, e.ccInputsFor(meta.Compile))
	case srcExtAsm:
		if isGoModuleType(e.d.unit.Type) && strings.HasSuffix(src.string(), ".s") {
			e.collectGoSource(meta, true)
		} else {
			e.emitLibraryAsmSource(meta, e.ccInputsFor(meta.Compile))
		}
	case srcExtYasm:
		e.emitLibraryYasmSource(meta, e.ccInputsFor(meta.Compile))
	case srcExtCuda:
		e.emitLibraryCudaSource(meta, e.ccInputsFor(meta.Compile))
	case srcExtGztProto:
		e.emitLibraryGztProtoCompile(src)
	case srcExtProto:
		if isGoModuleType(e.d.unit.Type) {
			e.emitGoProtoSource(meta)
		} else if isPythonProtoModule(e.d) {
			e.emitPyProtoSource(src, meta.Stmt, 0, meta.Prio, meta.Seq)
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
		} else if isPythonProtoModule(e.d) {
			e.emitPyProtoSource(src, meta.Stmt, 0, meta.Prio, meta.Seq)
		} else {
			e.emitLibraryEvSource(meta)
		}
	case srcExtRl:
		e.emitLibraryRagel5Source(meta)
	case srcExtFml:
		e.emitLibraryFmlSource(src)
	case srcExtSfdl:
		e.emitLibrarySfdlSource(src)
	case srcExtAsp:
		e.emitLibraryAspSource(meta)
	case srcExtFlex:
		e.emitLibraryFlexSource(meta)
	case srcExtHIn:
		e.emitLibraryHInSource(src)
	case srcExtCppIn, srcExtCIn:
		e.emitLibraryCInSource(meta)
	case srcExtSc:
		e.emitLibrarySCSource(src)
	case srcExtGperf:
		e.emitLibraryGperfSource(meta)
	case srcExtCfgProto:
		e.emitLibraryCfgProtoSource(meta)
	case srcExtSwg:
		e.emitLibrarySwigSource(meta)
	case srcExtSsqls:
		e.emitSsqlsSource(meta)
	default:
		if meta.Global && e.collectModuleGlobalSource(src) {
			return
		}

		if isGoModuleType(e.d.unit.Type) && src.string() == "CGO_EXPORT" {
			return
		}

		e.ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", e.instance.Path.relString(), src.string()),
		})
	}
}
