package main

import (
	"fmt"
	"strings"
)

func (e *EmitContext) emitOneSource(meta SrcMeta) {
	src := meta.Source

	switch srcExtClassOf(src) {
	case srcExtHeader:
		return
	case srcExtGztProto:
		e.emitLibraryGztProtoCompile(src)
	case srcExtProto:
		if e.d.unit.Tag == unitTagPy3Proto {
			e.emitPyProtoSource(meta.Source, 0)
		} else {
			e.emitCppProtoFamilySource(meta, cppProtoSpec)
		}
	case srcExtFbs64:
		e.emitLibraryFlatcSource(meta, &flatcVariantFL64)
	case srcExtFbs:
		e.emitLibraryFlatcSource(meta, &flatcVariantFL)
	case srcExtRodata:
		e.emitLibraryRodataSource(meta)
	case srcExtCSource:
		e.emitLibraryCSource(meta)
	case srcExtGo:
		if isGoModuleType(e.d.unit.Type) {
			e.collectGoSource(meta, false)
		}
	case srcExtAsm:
		if isGoModuleType(e.d.unit.Type) && strings.HasSuffix(meta.Source.string(), ".s") {
			e.collectGoSource(meta, true)
		} else {
			e.emitLibraryAsmSource(meta)
		}
	case srcExtYasm:
		e.emitLibraryYasmSource(meta)
	case srcExtCuda:
		e.emitLibraryCudaSource(meta)
	case srcExtRl6:
		e.emitLibraryRagel6Source(src)
	case srcExtY:
		e.emitBisonY(src)
	case srcExtEv:
		e.emitLibraryEvSource(meta)
	case srcExtRl:
		e.emitLibraryRagel5Source(src)
	case srcExtFlex:
		e.emitLibraryFlexSource(src)
	case srcExtHIn:
		e.emitLibraryHInSource(src)
	case srcExtCppIn, srcExtCIn:
		e.emitLibraryCInSource(meta)
	case srcExtSc:
		e.emitLibrarySCSource(src)
	case srcExtGperf:
		e.emitLibraryGperfSource(src)
	case srcExtCfgProto:
		e.emitLibraryCfgProtoSource(meta)
	default:
		if isGoModuleType(e.d.unit.Type) && src.string() == "CGO_EXPORT" {
			return
		}

		e.ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", e.instance.Path.rel(), src.string()),
		})
	}
}
