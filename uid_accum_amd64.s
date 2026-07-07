//go:build amd64

#include "textflag.h"

// func uidAccumAVX2(p *uint64, n int) (sum, xr, sq, cb uint64)
// n must be a positive multiple of 4.
// low64(e*e)  = lo*lo + ((lo*hi) << 33)            for e = hi<<32 | lo.
// low64(t*e)  = lo_t*lo + ((lo_t*hi + hi_t*lo) << 32)  for t = low64(e*e).
TEXT ·uidAccumAVX2(SB), NOSPLIT, $0-48
	MOVQ  p+0(FP), SI
	MOVQ  n+8(FP), CX
	VPXOR Y0, Y0, Y0
	VPXOR Y1, Y1, Y1
	VPXOR Y2, Y2, Y2
	VPXOR Y10, Y10, Y10

loop:
	VMOVDQU  (SI), Y3
	VPADDQ   Y3, Y0, Y0
	VPXOR    Y3, Y1, Y1
	VPSRLQ   $32, Y3, Y4
	VPMULUDQ Y3, Y4, Y5
	VPSLLQ   $33, Y5, Y5
	VPMULUDQ Y3, Y3, Y6
	VPADDQ   Y6, Y5, Y5
	VPADDQ   Y5, Y2, Y2
	VPSRLQ   $32, Y5, Y7
	VPMULUDQ Y5, Y3, Y8
	VPMULUDQ Y5, Y4, Y9
	VPMULUDQ Y7, Y3, Y7
	VPADDQ   Y9, Y7, Y7
	VPSLLQ   $32, Y7, Y7
	VPADDQ   Y8, Y7, Y7
	VPADDQ   Y7, Y10, Y10
	ADDQ     $32, SI
	SUBQ     $4, CX
	JNZ      loop

	VEXTRACTI128 $1, Y0, X4
	VPADDQ       X4, X0, X0
	VPSHUFD      $0x4E, X0, X4
	VPADDQ       X4, X0, X0
	VMOVQ        X0, AX

	VEXTRACTI128 $1, Y1, X4
	VPXOR        X4, X1, X1
	VPSHUFD      $0x4E, X1, X4
	VPXOR        X4, X1, X1
	VMOVQ        X1, BX

	VEXTRACTI128 $1, Y2, X4
	VPADDQ       X4, X2, X2
	VPSHUFD      $0x4E, X2, X4
	VPADDQ       X4, X2, X2
	VMOVQ        X2, DX

	VEXTRACTI128 $1, Y10, X4
	VPADDQ       X4, X10, X10
	VPSHUFD      $0x4E, X10, X4
	VPADDQ       X4, X10, X10
	VMOVQ        X10, R8

	VZEROUPPER
	MOVQ AX, sum+16(FP)
	MOVQ BX, xr+24(FP)
	MOVQ DX, sq+32(FP)
	MOVQ R8, cb+40(FP)
	RET
