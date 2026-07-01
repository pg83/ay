package main

import (
	"fmt"
)

var srcExtMatcher *ExtMatcher[SrcEmitter]

type SrcEmitter = func(e *EmitContext, src STR)

func init() {
	srcExtMatcher = newExtMatcher([]ExtEntry[SrcEmitter]{
		{".gztproto", (*EmitContext).emitLibraryGztProtoCompile},
		{".proto", (*EmitContext).emitLibraryProtoSource},
		{".fbs64", (*EmitContext).emitLibraryFlatcSource},
		{".fbs", (*EmitContext).emitLibraryFlatcSource},
		{".rodata", (*EmitContext).emitLibraryRodataSource},
		{".c", (*EmitContext).emitLibraryCSource},
		{".cpp", (*EmitContext).emitLibraryCSource},
		{".cc", (*EmitContext).emitLibraryCSource},
		{".cxx", (*EmitContext).emitLibraryCSource},
		{".C", (*EmitContext).emitLibraryCSource},
		{".auxcpp", (*EmitContext).emitLibraryCSource},
		{".S", (*EmitContext).emitLibraryAsmSource},
		{".s", (*EmitContext).emitLibraryAsmSource},
		{".asm", (*EmitContext).emitLibraryAsmSource},
		{".cu", (*EmitContext).emitLibraryCudaSource},
		{".rl6", (*EmitContext).emitLibraryRagel6Source},
		{".y", (*EmitContext).emitBisonY},
		{".ypp", (*EmitContext).emitBisonY},
		{".ev", (*EmitContext).emitLibraryEvSource},
		{".rl", (*EmitContext).emitLibraryRagel5Source},
		{".lpp", (*EmitContext).emitLibraryFlexSource},
		{".lex", (*EmitContext).emitLibraryFlexSource},
		{".l", (*EmitContext).emitLibraryFlexSource},
		{".h.in", (*EmitContext).emitLibraryHInSource},
		{".cpp.in", (*EmitContext).emitLibraryCInSource},
		{".c.in", (*EmitContext).emitLibraryCInSource},
		{".sc", (*EmitContext).emitLibrarySCSource},
		{".gperf", (*EmitContext).emitLibraryGperfSource},
		{".cfgproto", (*EmitContext).emitLibraryCfgProtoSource},
	})
}

func (e *EmitContext) emitOneSource(src STR) {
	ctx, instance, _ := e.ctx, e.instance, e.d
	srcRel := src.string()

	if isHeaderSource(srcRel) {
		return
	}

	emit, ok := srcExtMatcher.match(srcRel)

	if !ok {
		ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", instance.Path.rel(), srcRel),
		})

		return
	}

	emit(e, src)
}
