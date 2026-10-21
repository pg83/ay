package main

var pireInlineKV = KV{P: pkPI, PC: pcYellow}

func (e *EmitContext) emitPireInline(input, output VFS) {
	e.emitRunProgramStmt(&RunProgramStmt{
		KV:             &pireInlineKV,
		ToolPath:       strLibraryCppRegexPireInline.any(),
		Args:           []ANY{argDashO.any(), output.any(), input.any()},
		INFiles:        []ANY{input.any()},
		OUTFiles:       []ANY{output.any()},
		OutputIncludes: []ANY{output.any()},
	})
}
