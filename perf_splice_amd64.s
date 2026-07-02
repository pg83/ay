#include "textflag.h"

#define DIST 24

TEXT ·spliceKernelAVX512(SB), NOSPLIT, $0-56
	MOVQ         gen+0(FP), AX
	MOVL         epoch+8(FP), CX
	MOVQ         win+16(FP), BX
	MOVQ         n+24(FP), DX
	MOVQ         block+32(FP), SI
	MOVQ         k+40(FP), DI

	ANDQ         $-16, DX
	VPBROADCASTD CX, Z1
	MOVL         $0xffff, R8
	XORQ         R9, R9

avxloop:
	CMPQ         R9, DX
	JGE          avxdone
	KMOVW        R8, K1
	VMOVDQU32    (BX)(R9*4), Z0
	VPGATHERDD   (AX)(Z0*4), K1, Z2
	VPCMPD       $4, Z1, Z2, K2
	VPCOMPRESSD  Z0, K2, (SI)(DI*4)
	KMOVW        K2, R10
	POPCNTL      R10, R10
	VPSCATTERDD  Z1, K2, (AX)(Z0*4)
	ADDQ         R10, DI
	ADDQ         $16, R9
	JMP          avxloop

avxdone:
	MOVQ         DI, ret+48(FP)
	VZEROUPPER
	RET

TEXT ·spliceKernelPrefetch(SB), NOSPLIT, $0-56
	MOVQ  gen+0(FP), AX
	MOVL  epoch+8(FP), CX
	MOVQ  win+16(FP), BX
	MOVQ  n+24(FP), DX
	MOVQ  block+32(FP), SI
	MOVQ  k+40(FP), DI
	XORQ  R9, R9
	JMP   pftest

pfloop:
	LEAQ       DIST(R9), R10
	CMPQ       R10, DX
	JGE        pfnopre
	MOVL       (BX)(R10*4), R11
	PREFETCHT0 (AX)(R11*4)

pfnopre:
	MOVL  (BX)(R9*4), R10
	MOVL  (AX)(R10*4), R11
	CMPL  R11, CX
	JE    pfnext
	MOVL  CX, (AX)(R10*4)
	MOVL  R10, (SI)(DI*4)
	INCQ  DI

pfnext:
	INCQ  R9

pftest:
	CMPQ  R9, DX
	JL    pfloop

	MOVQ  DI, ret+48(FP)
	RET
