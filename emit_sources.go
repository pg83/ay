package main

import (
	"fmt"
	"strings"
)

type SourceEmit struct {
	Ref     NodeRef
	OutPath VFS
	Extra   []SourceEmit
}

func emitOneSource(ctx *GenCtx, instance ModuleInstance, d *ModuleData, srcRel string, in ModuleCCInputs) *SourceEmit {
	if isHeaderSource(srcRel) {
		return nil
	}

	switch {
	case strings.HasSuffix(srcRel, ".gztproto"):
		return emitLibraryGztProtoCompile(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".proto"):
		return emitLibraryProtoSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".fbs64"),
		strings.HasSuffix(srcRel, ".fbs"):
		return emitLibraryFlatcSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".rodata"):
		return emitLibraryRodataSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".c"),
		strings.HasSuffix(srcRel, ".cpp"),
		strings.HasSuffix(srcRel, ".cc"),
		strings.HasSuffix(srcRel, ".cxx"),

		strings.HasSuffix(srcRel, ".C"):
		return emitLibraryCSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".S"),
		strings.HasSuffix(srcRel, ".s"),
		strings.HasSuffix(srcRel, ".asm"):
		return emitLibraryAsmSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".rl6"):
		return emitLibraryRagel6Source(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".y"),
		strings.HasSuffix(srcRel, ".ypp"):
		return emitBisonY(ctx, instance, srcRel, in, in.BisonGenExt)
	case strings.HasSuffix(srcRel, ".ev"):
		return emitLibraryEvSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".rl"):
		return emitLibraryRagel5Source(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".lpp"),
		strings.HasSuffix(srcRel, ".lex"),
		strings.HasSuffix(srcRel, ".l"):
		return emitLibraryFlexSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".h.in"):
		return emitLibraryHInSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".cpp.in"),
		strings.HasSuffix(srcRel, ".c.in"):
		return emitLibraryCInSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".sc"):
		return emitLibrarySCSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".gperf"):
		return emitLibraryGperfSource(ctx, instance, d, srcRel, in)
	case strings.HasSuffix(srcRel, ".cfgproto"):
		return emitLibraryCfgProtoSource(ctx, instance, d, srcRel, in)
	}

	ctx.onWarn(Warn{
		Kind:    WarnUnsupportedSource,
		Message: fmt.Sprintf("%s: unsupported source extension in %q", instance.Path.rel(), srcRel),
	})

	return nil
}
