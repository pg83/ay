package main

// codegen.go — Python / Enum / Resource generator-driven nodes.
//
// emitPySrcs:    one PY (.yapyc3) node per PY_SRCS source.
// emitPyRegister: PY+CC pair per PY_REGISTER() declaration.
// emitEnumSrcs:  EN+downstream-CC chain per GENERATE_ENUM_SERIALIZATION.
//
// All three drive through EmitEN / EmitPB / EmitEV-style codegen
// producers and the matching CodegenRegistry registration; the
// downstream CC for each is composed via EmitCC with IsGenerated=true.

// codegenRegForInstance returns the CodegenRegistry attached to the
// scanner picked by scannerFor (nil-safe).
func codegenRegForInstance(ctx *genCtx, instance ModuleInstance) *CodegenRegistry {
	sc := ctx.scannerFor(instance)
	if sc == nil {
		return nil
	}
	return sc.codegen
}
