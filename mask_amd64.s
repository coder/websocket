#include "textflag.h"

// func maskAsm(b *byte, len int, key uint32)
TEXT ·maskAsm(SB), NOSPLIT, $0-28
	// AX = b
	// CX = len (left length)
	// SI = key (uint32)
	// DI = uint64(SI) | uint64(SI)<<32
	MOVQ b+0(FP), AX
	MOVQ len+8(FP), CX
	MOVL key+16(FP), SI

	// Calculate the DI aka the uint64 key.
	// DI = uint64(SI) | uint64(SI)<<32
	MOVL SI, DI
	MOVQ DI, DX
	SHLQ $32, DI
	ORQ  DX, DI

	CMPQ  CX, $8
	JL    less_than_8
	CMPQ  CX, $128
	JLE   sse
	TESTQ $31, AX
	JNZ   unaligned

aligned:
	CMPB ·useAVX2(SB), $1
	JE   avx2
	JMP  sse

unaligned_loop_1byte:
	XORB  SI, (AX)
	INCQ  AX
	DECQ  CX
	ROLL  $24, SI
	TESTQ $7, AX
	JNZ   unaligned_loop_1byte

	// Calculate DI again since SI was modified.
	// DI = uint64(SI) | uint64(SI)<<32
	MOVL SI, DI
	MOVQ DI, DX
	SHLQ $32, DI
	ORQ  DX, DI

	TESTQ $31, AX
	JZ    aligned

unaligned:
	// $7 & len, if not zero jump to loop_1b.
	TESTQ $7, AX
	JNZ   unaligned_loop_1byte

unaligned_loop:
	// We don't need to check the CX since we know it's above 512.
	XORQ  DI, (AX)
	ADDQ  $8, AX
	SUBQ  $8, CX
	TESTQ $31, AX
	JNZ   unaligned_loop
	JMP   aligned

avx2:
	CMPQ         CX, $128
	JL           sse
	VMOVQ        DI, X0
	VPBROADCASTQ X0, Y0

// TODO: shouldn't these be aligned movs now?
// TODO: should be 256?
avx2_loop:
	VMOVDQU (AX), Y1
	VPXOR   Y0, Y1, Y2
	VMOVDQU Y2, (AX)
	ADDQ    $128, AX
	SUBQ    $128, CX
	CMPQ    CX, $128
	// Loop if CX >= 128.
	JAE     avx2_loop

// TODO: should be 128?
sse:
	CMPQ       CX, $64
	JL         less_than_64
	MOVQ       DI, X0
	PUNPCKLQDQ X0, X0

sse_loop:
	MOVOU (AX), X1
	MOVOU 16(AX), X2
	MOVOU 2*16(AX), X3
	MOVOU 3*16(AX), X4
	PXOR  X0, X1
	PXOR  X0, X2
	PXOR  X0, X3
	PXOR  X0, X4
	MOVOU X1, 0*16(AX)
	MOVOU X2, 1*16(AX)
	MOVOU X3, 2*16(AX)
	MOVOU X4, 3*16(AX)
	ADDQ  $64, AX
	SUBQ  $64, CX
	CMPQ  CX, $64
	JAE   sse_loop

less_than_64:
	TESTQ $32, CX
	JZ    less_than_32
	XORQ  DI, (AX)
	XORQ  DI, 8(AX)
	XORQ  DI, 16(AX)
	XORQ  DI, 24(AX)
	ADDQ  $32, AX

less_than_32:
	TESTQ $16, CX
	JZ    less_than_16
	XORQ  DI, (AX)
	XORQ  DI, 8(AX)
	ADDQ  $16, AX

less_than_16:
	TESTQ $8, CX
	JZ    less_than_8
	XORQ  DI, (AX)
	ADDQ  $8, AX

less_than_8:
	TESTQ $4, CX
	JZ    less_than_4
	XORL  SI, (AX)
	ADDQ  $4, AX

less_than_4:
	TESTQ $2, CX
	JZ    less_than_2
	XORW  SI, (AX)
	ROLL  $16, SI
	ADDQ  $2, AX

less_than_2:
	TESTQ $1, CX
	JZ    end
	XORB  SI, (AX)
	ROLL  $24, SI

end:
	MOVL SI, ret+24(FP)
	RET
