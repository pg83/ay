package main

import (
	"fmt"
)

var srcExtMatcher *ExtMatcher[SrcEmitter]

type SourceEmit struct {
	Ref     NodeRef
	OutPath VFS
	Extra   []SourceEmit
}

type SrcEmitter = func(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR) *SourceEmit

func init() {
	srcExtMatcher = newExtMatcher([]ExtEntry[SrcEmitter]{
		{".gztproto", emitLibraryGztProtoCompile},
		{".proto", emitLibraryProtoSource},
		{".fbs64", emitLibraryFlatcSource},
		{".fbs", emitLibraryFlatcSource},
		{".rodata", emitLibraryRodataSource},
		{".c", emitLibraryCSource},
		{".cpp", emitLibraryCSource},
		{".cc", emitLibraryCSource},
		{".cxx", emitLibraryCSource},
		{".C", emitLibraryCSource},
		{".auxcpp", emitLibraryCSource},
		{".S", emitLibraryAsmSource},
		{".s", emitLibraryAsmSource},
		{".asm", emitLibraryAsmSource},
		{".cu", emitLibraryCudaSource},
		{".rl6", emitLibraryRagel6Source},
		{".y", emitBisonY},
		{".ypp", emitBisonY},
		{".ev", emitLibraryEvSource},
		{".rl", emitLibraryRagel5Source},
		{".lpp", emitLibraryFlexSource},
		{".lex", emitLibraryFlexSource},
		{".l", emitLibraryFlexSource},
		{".h.in", emitLibraryHInSource},
		{".cpp.in", emitLibraryCInSource},
		{".c.in", emitLibraryCInSource},
		{".sc", emitLibrarySCSource},
		{".gperf", emitLibraryGperfSource},
		{".cfgproto", emitLibraryCfgProtoSource},
	})
}

func emitOneSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR) *SourceEmit {
	srcRel := src.string()

	if isHeaderSource(srcRel) {
		return nil
	}

	emit, ok := srcExtMatcher.match(srcRel)

	if !ok {
		ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", instance.Path.rel(), srcRel),
		})

		return nil
	}

	return emit(ctx, instance, d, src)
}
