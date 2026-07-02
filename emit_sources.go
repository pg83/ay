package main

import (
	"fmt"
)

func (e *EmitContext) emitOneSource(src STR) {
	switch srcExtClassOf(src) {
	case srcExtHeader:
		return
	case srcExtGztProto:
		e.emitLibraryGztProtoCompile(src)
	case srcExtProto:
		e.emitLibraryProtoSource(src)
	case srcExtFbs64:
		e.emitLibraryFlatcSource64(src)
	case srcExtFbs:
		e.emitLibraryFlatcSource32(src)
	case srcExtRodata:
		e.emitLibraryRodataSource(src)
	case srcExtCSource:
		e.emitLibraryCSource(src)
	case srcExtAsm:
		e.emitLibraryAsmSource(src)
	case srcExtYasm:
		e.emitLibraryYasmSource(src)
	case srcExtCuda:
		e.emitLibraryCudaSource(src)
	case srcExtRl6:
		e.emitLibraryRagel6Source(src)
	case srcExtY:
		e.emitBisonY(src)
	case srcExtEv:
		e.emitLibraryEvSource(src)
	case srcExtRl:
		e.emitLibraryRagel5Source(src)
	case srcExtFlex:
		e.emitLibraryFlexSource(src)
	case srcExtHIn:
		e.emitLibraryHInSource(src)
	case srcExtCppIn, srcExtCIn:
		e.emitLibraryCInSource(src)
	case srcExtSc:
		e.emitLibrarySCSource(src)
	case srcExtGperf:
		e.emitLibraryGperfSource(src)
	case srcExtCfgProto:
		e.emitLibraryCfgProtoSource(src)
	default:
		e.ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", e.instance.Path.rel(), src.string()),
		})
	}
}
