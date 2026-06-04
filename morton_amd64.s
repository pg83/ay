#include "textflag.h"

// func morton(p, s uint32) uint64
// s deposits into the even bit positions (mask 0x5555...), p into the odd ones
// (mask 0xAAAA...), then OR — the Z-order interleave of the two 32-bit ids.
TEXT ·morton(SB), NOSPLIT, $0-16
	MOVL  p+0(FP), AX        // zero-extends p into RAX
	MOVL  s+4(FP), BX        // zero-extends s into RBX
	MOVQ  $0x5555555555555555, CX
	MOVQ  $0xAAAAAAAAAAAAAAAA, DX
	PDEPQ CX, BX, BX         // BX = pdep(s, 0x5555...) -> even bits
	PDEPQ DX, AX, AX         // AX = pdep(p, 0xAAAA...) -> odd bits
	ORQ   AX, BX
	MOVQ  BX, ret+8(FP)
	RET
