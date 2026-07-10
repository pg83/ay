package main

import "fmt"

func (e *EmitContext) emitOneSource(meta SrcMeta) {
	src := meta.Source

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
	case srcExtGztProto:
		e.emitLibraryGztProtoCompile(src)
	case srcExtProto:
		if meta.PyProtoGroup != nil {
			e.emitPyProtoSource(src, *meta.PyProtoGroup)
		} else if e.d.unit.Tag == unitTagPy3Proto {
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
