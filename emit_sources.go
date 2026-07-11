package main

import (
	"fmt"
	"strings"
)

func (e *EmitContext) emitOneSource(meta SrcMeta) {
	src := meta.Source

	if meta.Py != nil {
		if meta.Py.Kind == pySourceProtoInput {
			e.emitPyProtoSource(src, meta.Py.Group)
		} else {
			e.collectPySource(src.vfs(), *meta.Py)
		}

		return
	}

	if e.producersOnly() {
		switch srcExtClassOf(src.relOrSelf().any()) {
		case srcExtCSource, srcExtRodata, srcExtCuda, srcExtYasm, srcExtAsm, srcExtGo:
			return
		case srcExtProto:
			if e.d.unit.Tag == unitTagPy3Proto {
				return
			}
		}
	}

	switch srcExtClassOf(src.relOrSelf().any()) {
	case srcExtHeader:
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
		if e.d.unit.Tag == unitTagPy3Proto {
			e.emitPyProtoSource(src, 0)
		} else {
			e.emitCppProtoFamilySource(meta, cppProtoSpec)
		}
	case srcExtFbs64:
		e.emitLibraryFlatcSource(meta, &flatcVariantFL64)
	case srcExtFbs:
		e.emitLibraryFlatcSource(meta, &flatcVariantFL)
	case srcExtGo:
		if isGoModuleType(e.d.unit.Type) {
			e.collectGoSource(meta, false)
		}
	case srcExtRl6:
		e.emitLibraryRagel6Source(meta)
	case srcExtY:
		e.emitBisonY(meta)
	case srcExtEv:
		e.emitLibraryEvSource(meta)
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
	default:
		if isGoModuleType(e.d.unit.Type) && src.string() == "CGO_EXPORT" {
			return
		}

		e.ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", e.instance.Path.relString(), src.string()),
		})
	}
}
