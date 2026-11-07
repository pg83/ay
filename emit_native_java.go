package main

type NativeJavaProgram struct {
	Call    JavaRunProgram
	DeclSeq int
}

func (e *EmitContext) emitNativeJavaProgram(program NativeJavaProgram) {
	dir := e.instance.Path.relString()
	r := parseJavaRunProgram(dir, program.Call)
	outputs := javaRunProgramOutputs(dir, r)

	if len(outputs) == 0 {
		throwFmt("RUN_JAVA_PROGRAM requires at least one output")
	}

	ref := e.ctx.emit.reserve()

	pending := e.pendingEmit(func() {
		e.ctx.emitJavaRunProgram(dir, e.instance.Platform, e.d.runJavaJdk, javaGlobalsOf(e.peers.ResourceGlobals), r, ref, e.runProgramInputVFS, e)
	})

	for _, output := range outputs {
		e.register(GeneratedFileSpec{OutputPath: output, ProducerRef: ref, ProducerMainOut: outputs[0], OnUse: pending})
	}

	for _, name := range r.out {
		if text := name.string(); isCodegenProducingSrc(text) || isNativeSourceExt(text) {
			e.enqueueSrc(SrcMeta{Source: copyFileOutputVFS(dir, text).any(), Prio: stmtPrioDefault})
		}
	}
}
