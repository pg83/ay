package main

import (
	"fmt"
)

type SourceEmit struct {
	Ref     NodeRef
	OutPath VFS
	Extra   []SourceEmit
}

type srcEmitter = func(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit

var srcExtMatcher *ExtMatcher[srcEmitter]

func init() {
	srcExtMatcher = NewExtMatcher([]ExtEntry[srcEmitter]{
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

func emitOneSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, src STR, in ModuleCCInputs) *SourceEmit {
	srcRel := src.string()

	if isHeaderSource(srcRel) {
		return nil
	}

	if v := src.vfs(); v != 0 {
		if info := codegenRegForInstance(ctx, instance).lookup(v); info != nil && info.Compile != nil {
			sp := info.Compile
			in.PerSourceCFlags = sp.CFlags
			in.FlatOutput = sp.FlatOutput
			in.Variant = sp.Variant
			in.ObjectSuffixStem = sp.ObjectSuffixStem
			in.Py3Suffix = sp.Py3Suffix
			in.ForceCxx = sp.ForceCxx
		}
	}

	emit, ok := srcExtMatcher.match(srcRel)

	if !ok {
		ctx.onWarn(Warn{
			Kind:    WarnUnsupportedSource,
			Message: fmt.Sprintf("%s: unsupported source extension in %q", instance.Path.rel(), srcRel),
		})

		return nil
	}

	return emit(ctx, instance, d, src, in)
}
