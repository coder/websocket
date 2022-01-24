#include "textflag.h"

// func maskAsm(b *byte,len, int, key uint32)
TEXT ·maskAsm(SB), NOSPLIT, $0-28
	// R0 = b
	// R1 = len
	// R2 = uint64(key)<<32 | uint64(key)
	// R3 = key (uint32)
	MOVD  b_ptr+0(FP), R0
	MOVD  b_len+8(FP), R1
	MOVWU key+16(FP), R3
	MOVD  R3, R2
	ORR   R2<<32, R2, R2
	VDUP  R2, V0.D2
	CMP   $64, R1
	BLT   less_than_64

	// todo: optimize unaligned case
loop_64:
	VLD1   (R0), [V1.B16, V2.B16, V3.B16, V4.B16]
	VEOR   V1.B16, V0.B16, V1.B16
	VEOR   V2.B16, V0.B16, V2.B16
	VEOR   V3.B16, V0.B16, V3.B16
	VEOR   V4.B16, V0.B16, V4.B16
	VST1.P [V1.B16, V2.B16, V3.B16, V4.B16], 64(R0)
	SUBS   $64, R1
	CMP    $64, R1
	BGE    loop_64

less_than_64:
	// quick end
	CBZ    R1, end
	TBZ    $5, R1, less_than32
	VLD1   (R0), [V1.B16, V2.B16]
	VEOR   V1.B16, V0.B16, V1.B16
	VEOR   V2.B16, V0.B16, V2.B16
	VST1.P [V1.B16, V2.B16], 32(R0)

less_than32:
	TBZ   $4, R1, less_than16
	LDP   (R0), (R11, R12)
	EOR   R11, R2, R11
	EOR   R12, R2, R12
	STP.P (R11, R12), 16(R0)

less_than16:
	TBZ    $3, R1, less_than8
	MOVD   (R0), R11
	EOR    R2, R11, R11
	MOVD.P R11, 8(R0)

less_than8:
	TBZ     $2, R1, less_than4
	MOVWU   (R0), R11
	EORW    R2, R11, R11
	MOVWU.P R11, 4(R0)

less_than4:
	TBZ     $1, R1, less_than2
	MOVHU   (R0), R11
	EORW    R3, R11, R11
	MOVHU.P R11, 2(R0)
	RORW    $16, R3

less_than2:
	TBZ     $0, R1, end
	MOVBU   (R0), R11
	EORW    R3, R11, R11
	MOVBU.P R11, 1(R0)
	RORW    $8, R3

end:
	MOVWU R3, ret+24(FP)
	RET
