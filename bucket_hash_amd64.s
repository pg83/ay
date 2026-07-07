//go:build amd64

#include "textflag.h"

TEXT ·cpuHasAVX2(SB), NOSPLIT, $0-1
	MOVL $1, AX
	CPUID
	TESTL $(1<<27), CX
	JZ   no
	TESTL $(1<<28), CX
	JZ   no
	XORL CX, CX
	XGETBV
	ANDL $6, AX
	CMPL AX, $6
	JNE  no
	MOVL $7, AX
	XORL CX, CX
	CPUID
	TESTL $(1<<5), BX
	JZ   no
	MOVB $1, ret+0(FP)
	RET

no:
	MOVB $0, ret+0(FP)
	RET

// func bucketAccumAVX2(p *VFS, n int) (sum, xr, sq, cb uint32)
// n must be a positive multiple of 8.
TEXT ·bucketAccumAVX2(SB), NOSPLIT, $0-32
	MOVQ  p+0(FP), SI
	MOVQ  n+8(FP), CX
	VPXOR Y0, Y0, Y0
	VPXOR Y1, Y1, Y1
	VPXOR Y2, Y2, Y2
	VPXOR Y5, Y5, Y5

loop:
	VMOVDQU (SI), Y3
	VPADDD  Y3, Y0, Y0
	VPXOR   Y3, Y1, Y1
	VPMULLD Y3, Y3, Y4
	VPADDD  Y4, Y2, Y2
	VPMULLD Y3, Y4, Y6
	VPADDD  Y6, Y5, Y5
	ADDQ    $32, SI
	SUBQ    $8, CX
	JNZ     loop

	VEXTRACTI128 $1, Y0, X4
	VPADDD       X4, X0, X0
	VPSHUFD      $0x4E, X0, X4
	VPADDD       X4, X0, X0
	VPSHUFD      $0xB1, X0, X4
	VPADDD       X4, X0, X0
	VMOVD        X0, AX

	VEXTRACTI128 $1, Y1, X4
	VPXOR        X4, X1, X1
	VPSHUFD      $0x4E, X1, X4
	VPXOR        X4, X1, X1
	VPSHUFD      $0xB1, X1, X4
	VPXOR        X4, X1, X1
	VMOVD        X1, BX

	VEXTRACTI128 $1, Y2, X4
	VPADDD       X4, X2, X2
	VPSHUFD      $0x4E, X2, X4
	VPADDD       X4, X2, X2
	VPSHUFD      $0xB1, X2, X4
	VPADDD       X4, X2, X2
	VMOVD        X2, DX

	VEXTRACTI128 $1, Y5, X4
	VPADDD       X4, X5, X5
	VPSHUFD      $0x4E, X5, X4
	VPADDD       X4, X5, X5
	VPSHUFD      $0xB1, X5, X4
	VPADDD       X4, X5, X5
	VMOVD        X5, R8

	VZEROUPPER
	MOVL AX, sum+16(FP)
	MOVL BX, xr+20(FP)
	MOVL DX, sq+24(FP)
	MOVL R8, cb+28(FP)
	RET
