//go:build (windows || unix) && amd64 && !disable_jit && !purego

package randomx

import (
	"encoding/binary"

	"cvn_node/internal/randomx/internal/asm"
)

const supportsJIT = true

/*

	REGISTER ALLOCATION:

	; rax -> temporary
	; rbx -> todo: iteration counter "ic"
	; rcx -> temporary
	; rdx -> temporary
	; TODO golang ABI1: closure context pointer
	; rsi -> scratchpad pointer
	; rdi -> todo: dataset pointer
	; rbp -> (do not use, it's used by Golang sampling) jump target //todo: memory registers "ma" (high 32 bits), "mx" (low 32 bits)
	; TODO golang ABI1: frame pointer
	; rsp -> stack pointer
	; TODO golang ABI1: stack pointer
	; r8  -> "r0"
	; r9  -> "r1"
	; r10 -> "r2"
	; r11 -> "r3"
	; r12 -> "r4"
	; r13 -> "r5"
	; r14 -> "r6"
	; TODO golang ABI1: g pointer
	; r15 -> "r7"
	; TODO golang ABI1: got reference if dynamic linked
	; xmm0 -> "f0"
	; xmm1 -> "f1"
	; xmm2 -> "f2"
	; xmm3 -> "f3"
	; xmm4 -> "e0"
	; xmm5 -> "e1"
	; xmm6 -> "e2"
	; xmm7 -> "e3"
	; xmm8 -> "a0"
	; xmm9 -> "a1"
	; xmm10 -> "a2"
	; xmm11 -> "a3"
	; xmm12 -> temporary
	; xmm13 -> E 'and' mask = 0x00ffffffffffffff00ffffffffffffff
	; xmm14 -> E 'or' mask  = 0x3*00000000******3*00000000******
	; xmm15 -> scale mask   = 0x81f000000000000081f0000000000000
	; TODO golang ABI1: zero value

*/

const MaxRandomXInstrCodeSize = 32   //FDIV_M requires up to 32 bytes of x86 code
const MaxSuperscalarInstrSize = 14   //IMUL_RCP requires 14 bytes of x86 code
const SuperscalarProgramHeader = 128 //overhead per superscalar program
const CodeAlign = 4096               //align code size to a multiple of 4 KiB
const ReserveCodeSize = CodeAlign    //function prologue/epilogue + reserve

func alignSize[T ~uintptr | ~uint32 | ~uint64 | ~int64 | ~int32 | ~int](pos, align T) T {
	return ((pos-1)/align + 1) * align
}

var RandomXCodeSize = alignSize[uint64](ReserveCodeSize+MaxRandomXInstrCodeSize*RANDOMX_PROGRAM_SIZE, CodeAlign)
var SuperscalarSize = alignSize[uint64](ReserveCodeSize+(SuperscalarProgramHeader+MaxSuperscalarInstrSize*SuperscalarMaxSize)*RANDOMX_CACHE_ACCESSES, CodeAlign)

var CodeSize = uint32(RandomXCodeSize + SuperscalarSize)

var superScalarHashOffset = int32(RandomXCodeSize)

var REX_ADD_RR = []byte{0x4d, 0x03}
var REX_ADD_RM = []byte{0x4c, 0x03}
var REX_SUB_RR = []byte{0x4d, 0x2b}
var REX_SUB_RM = []byte{0x4c, 0x2b}
var REX_MOV_RR = []byte{0x41, 0x8b}
var REX_MOV_RR64 = []byte{0x49, 0x8b}
var REX_MOV_R64R = []byte{0x4c, 0x8b}
var REX_IMUL_RR = []byte{0x4d, 0x0f, 0xaf}
var REX_IMUL_RRI = []byte{0x4d, 0x69}
var REX_IMUL_RM = []byte{0x4c, 0x0f, 0xaf}
var REX_MUL_R = []byte{0x49, 0xf7}
var REX_MUL_M = []byte{0x48, 0xf7}
var REX_81 = []byte{0x49, 0x81}
var AND_EAX_I byte = 0x25

var MOV_EAX_I byte = 0xb8

var MOV_RAX_I = []byte{0x48, 0xb8}
var MOV_RCX_I = []byte{0x48, 0xb9}
var REX_LEA = []byte{0x4f, 0x8d}
var REX_MUL_MEM = []byte{0x48, 0xf7, 0x24, 0x0e}
var REX_IMUL_MEM = []byte{0x48, 0xf7, 0x2c, 0x0e}
var REX_SHR_RAX = []byte{0x48, 0xc1, 0xe8}
var RAX_ADD_SBB_1 = []byte{0x48, 0x83, 0xC0, 0x01, 0x48, 0x83, 0xD8, 0x00}
var MUL_RCX = []byte{0x48, 0xf7, 0xe1}
var REX_SHR_RDX = []byte{0x48, 0xc1, 0xea}
var REX_SH = []byte{0x49, 0xc1}
var MOV_RCX_RAX_SAR_RCX_63 = []byte{0x48, 0x89, 0xc1, 0x48, 0xc1, 0xf9, 0x3f}
var AND_ECX_I = []byte{0x81, 0xe1}
var ADD_RAX_RCX = []byte{0x48, 0x01, 0xC8}
var SAR_RAX_I8 = []byte{0x48, 0xC1, 0xF8}
var NEG_RAX = []byte{0x48, 0xF7, 0xD8}
var ADD_R_RAX = []byte{0x4C, 0x03}
var XOR_EAX_EAX = []byte{0x33, 0xC0}
var ADD_RDX_R = []byte{0x4c, 0x01}
var SUB_RDX_R = []byte{0x4c, 0x29}
var SAR_RDX_I8 = []byte{0x48, 0xC1, 0xFA}
var TEST_RDX_RDX = []byte{0x48, 0x85, 0xD2}
var SETS_AL_ADD_RDX_RAX = []byte{0x0F, 0x98, 0xC0, 0x48, 0x03, 0xD0}
var REX_NEG = []byte{0x49, 0xF7}
var REX_XOR_RR = []byte{0x4D, 0x33}
var REX_XOR_RI = []byte{0x49, 0x81}
var REX_XOR_RM = []byte{0x4c, 0x33}
var REX_ROT_CL = []byte{0x49, 0xd3}
var REX_ROT_I8 = []byte{0x49, 0xc1}
var SHUFPD = []byte{0x66, 0x0f, 0xc6}
var REX_ADDPD = []byte{0x66, 0x41, 0x0f, 0x58}
var REX_CVTDQ2PD_XMM12 = []byte{0xf3, 0x44, 0x0f, 0xe6, 0x24, 0x06}
var REX_SUBPD = []byte{0x66, 0x41, 0x0f, 0x5c}
var REX_XORPS = []byte{0x41, 0x0f, 0x57}
var REX_MULPD = []byte{0x66, 0x41, 0x0f, 0x59}
var REX_MAXPD = []byte{0x66, 0x41, 0x0f, 0x5f}
var REX_DIVPD = []byte{0x66, 0x41, 0x0f, 0x5e}
var SQRTPD = []byte{0x66, 0x0f, 0x51}
var AND_OR_MOV_LDMXCSR = []byte{0x25, 0x00, 0x60, 0x00, 0x00, 0x0D, 0xC0, 0x9F, 0x00, 0x00, 0x50, 0x0F, 0xAE, 0x14, 0x24, 0x58}
var TEST_EAX_60SL13 = []byte{0xa9, 0x00, 0x80, 0x07, 0x00}

var ROL_RAX = []byte{0x48, 0xc1, 0xc0}
var XOR_ECX_ECX = []byte{0x33, 0xC9}
var REX_CMP_R32I = []byte{0x41, 0x81}
var REX_CMP_M32I = []byte{0x81, 0x3c, 0x06}
var MOVAPD = []byte{0x66, 0x0f, 0x29}
var REX_MOV_MR = []byte{0x4c, 0x89}
var REX_XOR_EAX = []byte{0x41, 0x33}
var SUB_EBX = []byte{0x83, 0xEB, 0x01}
var JNZ = []byte{0x0f, 0x85}
var JNZ_SHORT byte = 0x75
var JMP byte = 0xe9

var REX_XOR_RAX_R64 = []byte{0x49, 0x33}
var REX_XCHG = []byte{0x4d, 0x87}
var REX_ANDPS_XMM12 = []byte{0x45, 0x0F, 0x54, 0xE5, 0x45, 0x0F, 0x56, 0xE6}
var REX_PADD = []byte{0x66, 0x44, 0x0f}
var PADD_OPCODES = []byte{0xfc, 0xfd, 0xfe, 0xd4}
var CALL = 0xe8

var REX_ADD_I = []byte{0x49, 0x81}
var REX_TEST = []byte{0x49, 0xF7}
var JZ = []byte{0x0f, 0x84}
var JZ_SHORT byte = 0x74

var RET byte = 0xc3

var LEA_32 = []byte{0x41, 0x8d}
var MOVNTI = []byte{0x4c, 0x0f, 0xc3}
var ADD_EBX_I = []byte{0x81, 0xc3}

var NOP1 = []byte{0x90}
var NOP2 = []byte{0x66, 0x90}
var NOP3 = []byte{0x66, 0x66, 0x90}
var NOP4 = []byte{0x0F, 0x1F, 0x40, 0x00}
var NOP5 = []byte{0x0F, 0x1F, 0x44, 0x00, 0x00}
var NOP6 = []byte{0x66, 0x0F, 0x1F, 0x44, 0x00, 0x00}
var NOP7 = []byte{0x0F, 0x1F, 0x80, 0x00, 0x00, 0x00, 0x00}
var NOP8 = []byte{0x0F, 0x1F, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00}

var NOPX = [][]byte{NOP1, NOP2, NOP3, NOP4, NOP5, NOP6, NOP7, NOP8}

var JMP_ALIGN_PREFIX = [14][]byte{
	{},
	{0x2E},
	{0x2E, 0x2E},
	{0x2E, 0x2E, 0x2E},
	{0x2E, 0x2E, 0x2E, 0x2E},
	{0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x90, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x66, 0x90, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x66, 0x66, 0x90, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x0F, 0x1F, 0x40, 0x00, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
	{0x0F, 0x1F, 0x44, 0x00, 0x00, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E, 0x2E},
}

func genSIB(scale, index, base int) byte {
	return byte((scale << 6) | (index << 3) | base)
}
func genAddressReg(buf []byte, instr *ByteCodeInstruction, rax bool) []byte {
	buf = append(buf, LEA_32...)
	if rax {
		buf = append(buf, 0x80+instr.Src+0)
	} else {
		buf = append(buf, 0x80+instr.Src+8)
	}
	if instr.Src == RegisterNeedsSib {
		buf = append(buf, 0x24)
	}
	buf = binary.LittleEndian.AppendUint32(buf, uint32(instr.Imm))
	if rax {
		buf = append(buf, AND_EAX_I)
	} else {
		buf = append(buf, AND_ECX_I...)
	}
	buf = binary.LittleEndian.AppendUint32(buf, instr.MemMask)
	return buf
}

func valAsString(values ...uint32) []byte {
	r := make([]byte, 4*len(values))
	for i, v := range values {
		dst := r[i*4:]
		dst[0] = byte(v & 0xff)
		dst[1] = byte((v >> 8) & 0xff)
		dst[2] = byte((v >> 16) & 0xff)
		dst[3] = byte((v >> 24) & 0xff)
		switch {
		case dst[0] == 0:
			return r[:i*4]
		case dst[1] == 0:
			return r[:i*4+1]
		case dst[2] == 0:
			return r[:i*4+2]
		case dst[3] == 0:
			return r[:i*4+3]
		}
	}
	return r
}

func familyModel(maxFunctionId uint32) (family, model, stepping int) {
	if maxFunctionId < 0x1 {
		return 0, 0, 0
	}
	eax, _, _, _ := asm.Cpuid(1)
	// If BaseFamily[3:0] is less than Fh then ExtendedFamily[7:0] is reserved and Family is equal to BaseFamily[3:0].
	family = int((eax >> 8) & 0xf)
	extFam := family == 0x6 // Intel is 0x6, needs extended model.
	if family == 0xf {
		// Add ExtFamily
		family += int((eax >> 20) & 0xff)
		extFam = true
	}
	// If BaseFamily[3:0] is less than 0Fh then ExtendedModel[3:0] is reserved and Model is equal to BaseModel[3:0].
	model = int((eax >> 4) & 0xf)
	if extFam {
		// Add ExtModel
		model += int((eax >> 12) & 0xf0)
	}
	stepping = int(eax & 0xf)
	return family, model, stepping
}

var BranchesWithin32B = func() bool {
	a, b, c, d := asm.Cpuid(0)
	v := string(valAsString(b, d, c))

	if v == "GenuineIntel" {
		family, model, stepping := familyModel(a)

		// Intel JCC erratum mitigation
		if family == 6 {
			// Affected CPU models and stepping numbers are taken from https://www.intel.com/content/dam/support/us/en/documents/processors/mitigations-jump-conditional-code-erratum.pdf
			return ((model == 0x4E) && (stepping == 0x3)) ||
				((model == 0x55) && ((stepping == 0x4) || (stepping == 0x7))) ||
				((model == 0x5E) && (stepping == 0x3)) ||
				((model == 0x8E) && (stepping >= 0x9) && (stepping <= 0xC)) ||
				((model == 0x9E) && (stepping >= 0x9) && (stepping <= 0xD)) ||
				((model == 0xA6) && (stepping == 0x0)) ||
				((model == 0xAE) && (stepping == 0xA))
		}
	}
	return false
}()
