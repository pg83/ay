package main

import (
	"fmt"
)

func (e *EmitContext) emitOneSource(meta SrcMeta) {
	src := meta.Source

	switch srcExtClassOf(src) {
	case srcExtHeader:
		return
	case srcExtGztProto:
		e.emitLibraryGztProtoCompile(src)
	case srcExtProto:
		e.emitLibraryProtoSource(meta)
	case srcExtFbs64:
		e.emitLibraryFlatcSource64(src)
	case srcExtFbs:
		e.emitLibraryFlatcSource32(src)
	case srcExtRodata:
		e.emitLibraryRodataSource(meta)
	case srcExtCSource:
		e.emitLibraryCSource(meta)
	case srcExtAsm:
		e.emitLibraryAsmSource(meta)
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
		e.ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", e.instance.Path.rel(), src.string()),
		})
	}
}
