//go:build (windows || unix) && amd64 && !disable_jit && !purego

package randomx

import (
	"encoding/binary"
	"math/bits"
	"runtime"
	"unsafe"
)

//go:noescape
//go:nosplit
func vm_run(rf *RegisterFile, pad *ScratchPad, eMask [2]uint64, jmp uintptr)

//go:noescape
//go:nosplit
func vm_run_full(rf *RegisterFile, pad *ScratchPad, dataset *RegisterLine, iterations, memoryRegisters uint64, eMask [2]uint64, jmp uintptr)

/*
#define RANDOMX_DATASET_BASE_SIZE 2147483648
#define RANDOMX_DATASET_BASE_MASK    (RANDOMX_DATASET_BASE_SIZE-64)

mov ecx, ebp                       ;# ecx = ma
;#and ecx, RANDOMX_DATASET_BASE_MASK
and ecx, 2147483584
xor r8, qword ptr [rdi+rcx]
ror rbp, 32                        ;# swap "ma" and "mx"
xor rbp, rax                       ;# modify "mx"
mov edx, ebp                       ;# edx = mx
;#and edx, RANDOMX_DATASET_BASE_MASK
and edx, 2147483584
prefetchnta byte ptr [rdi+rdx]
xor r9,  qword ptr [rdi+rcx+8]
xor r10, qword ptr [rdi+rcx+16]
xor r11, qword ptr [rdi+rcx+24]
xor r12, qword ptr [rdi+rcx+32]
xor r13, qword ptr [rdi+rcx+40]
xor r14, qword ptr [rdi+rcx+48]
xor r15, qword ptr [rdi+rcx+56]
*/
var programReadDataset = []byte{
	0x89, 0xE9,
	0x81, 0xE1, 0xC0, 0xFF, 0xFF, 0x7F,
	0x4C, 0x33, 0x04, 0x0F,
	0x48, 0xC1, 0xCD, 0x20,
	0x48, 0x31, 0xC5,
	0x89, 0xEA,
	0x81, 0xE2, 0xC0, 0xFF, 0xFF, 0x7F,
	0x0F, 0x18, 0x04, 0x17,
	0x4C, 0x33, 0x4C, 0x0F, 0x08,
	0x4C, 0x33, 0x54, 0x0F, 0x10,
	0x4C, 0x33, 0x5C, 0x0F, 0x18,
	0x4C, 0x33, 0x64, 0x0F, 0x20,
	0x4C, 0x33, 0x6C, 0x0F, 0x28,
	0x4C, 0x33, 0x74, 0x0F, 0x30,
	0x4C, 0x33, 0x7C, 0x0F, 0x38,
}

/*
lea rcx, [rsi+rax]
push rcx
xor r8,  qword ptr [rcx+0]
xor r9,  qword ptr [rcx+8]
xor r10, qword ptr [rcx+16]
xor r11, qword ptr [rcx+24]
xor r12, qword ptr [rcx+32]
xor r13, qword ptr [rcx+40]
xor r14, qword ptr [rcx+48]
xor r15, qword ptr [rcx+56]
lea rcx, [rsi+rdx]
push rcx
cvtdq2pd xmm0, qword ptr [rcx+0]
cvtdq2pd xmm1, qword ptr [rcx+8]
cvtdq2pd xmm2, qword ptr [rcx+16]
cvtdq2pd xmm3, qword ptr [rcx+24]
cvtdq2pd xmm4, qword ptr [rcx+32]
cvtdq2pd xmm5, qword ptr [rcx+40]
cvtdq2pd xmm6, qword ptr [rcx+48]
cvtdq2pd xmm7, qword ptr [rcx+56]
andps xmm4, xmm13
andps xmm5, xmm13
andps xmm6, xmm13
andps xmm7, xmm13
orps xmm4, xmm14
orps xmm5, xmm14
orps xmm6, xmm14
orps xmm7, xmm14
*/
var programLoopLoad = []byte{
	0x48, 0x8D, 0x0C, 0x06,
	0x51,
	0x4C, 0x33, 0x01,
	0x4C, 0x33, 0x49, 0x08,
	0x4C, 0x33, 0x51, 0x10,
	0x4C, 0x33, 0x59, 0x18,
	0x4C, 0x33, 0x61, 0x20,
	0x4C, 0x33, 0x69, 0x28,
	0x4C, 0x33, 0x71, 0x30,
	0x4C, 0x33, 0x79, 0x38,
	0x48, 0x8D, 0x0C, 0x16,
	0x51,
	0xF3, 0x0F, 0xE6, 0x01,
	0xF3, 0x0F, 0xE6, 0x49, 0x08,
	0xF3, 0x0F, 0xE6, 0x51, 0x10,
	0xF3, 0x0F, 0xE6, 0x59, 0x18,
	0xF3, 0x0F, 0xE6, 0x61, 0x20,
	0xF3, 0x0F, 0xE6, 0x69, 0x28,
	0xF3, 0x0F, 0xE6, 0x71, 0x30,
	0xF3, 0x0F, 0xE6, 0x79, 0x38,
	0x41, 0x0F, 0x54, 0xE5,
	0x41, 0x0F, 0x54, 0xED,
	0x41, 0x0F, 0x54, 0xF5,
	0x41, 0x0F, 0x54, 0xFD,
	0x41, 0x0F, 0x56, 0xE6,
	0x41, 0x0F, 0x56, 0xEE,
	0x41, 0x0F, 0x56, 0xF6,
	0x41, 0x0F, 0x56, 0xFE,
}

/*
pop rcx
mov qword ptr [rcx+0], r8
mov qword ptr [rcx+8], r9
mov qword ptr [rcx+16], r10
mov qword ptr [rcx+24], r11
mov qword ptr [rcx+32], r12
mov qword ptr [rcx+40], r13
mov qword ptr [rcx+48], r14
mov qword ptr [rcx+56], r15
pop rcx

xorpd xmm0, xmm4
xorpd xmm1, xmm5
xorpd xmm2, xmm6
xorpd xmm3, xmm7

;# aligned mode
movapd xmmword ptr [rcx+0], xmm0
movapd xmmword ptr [rcx+16], xmm1
movapd xmmword ptr [rcx+32], xmm2
movapd xmmword ptr [rcx+48], xmm3
*/
var programLoopStoreAligned = []byte{
	0x59,
	0x4C, 0x89, 0x01,
	0x4C, 0x89, 0x49, 0x08,
	0x4C, 0x89, 0x51, 0x10,
	0x4C, 0x89, 0x59, 0x18,
	0x4C, 0x89, 0x61, 0x20,
	0x4C, 0x89, 0x69, 0x28,
	0x4C, 0x89, 0x71, 0x30,
	0x4C, 0x89, 0x79, 0x38,
	0x59,
	0x66, 0x0F, 0x57, 0xC4,
	0x66, 0x0F, 0x57, 0xCD,
	0x66, 0x0F, 0x57, 0xD6,
	0x66, 0x0F, 0x57, 0xDF,
	0x66, 0x0F, 0x29, 0x01,
	0x66, 0x0F, 0x29, 0x49, 0x10,
	0x66, 0x0F, 0x29, 0x51, 0x20,
	0x66, 0x0F, 0x29, 0x59, 0x30,
}

/*
pop rcx
mov qword ptr [rcx+0], r8
mov qword ptr [rcx+8], r9
mov qword ptr [rcx+16], r10
mov qword ptr [rcx+24], r11
mov qword ptr [rcx+32], r12
mov qword ptr [rcx+40], r13
mov qword ptr [rcx+48], r14
mov qword ptr [rcx+56], r15
pop rcx

aesenc xmm0, xmm4
aesdec xmm1, xmm4
aesenc xmm2, xmm4
aesdec xmm3, xmm4
aesenc xmm0, xmm5
aesdec xmm1, xmm5
aesenc xmm2, xmm5
aesdec xmm3, xmm5
aesenc xmm0, xmm6
aesdec xmm1, xmm6
aesenc xmm2, xmm6
aesdec xmm3, xmm6
aesenc xmm0, xmm7
aesdec xmm1, xmm7
aesenc xmm2, xmm7
aesdec xmm3, xmm7

;# aligned mode
movapd xmmword ptr [rcx+0], xmm0
movapd xmmword ptr [rcx+16], xmm1
movapd xmmword ptr [rcx+32], xmm2
movapd xmmword ptr [rcx+48], xmm3
*/
var programLoopStoreHardAES = []byte{
	0x59,
	0x4C, 0x89, 0x01,
	0x4C, 0x89, 0x49, 0x08,
	0x4C, 0x89, 0x51, 0x10,
	0x4C, 0x89, 0x59, 0x18,
	0x4C, 0x89, 0x61, 0x20,
	0x4C, 0x89, 0x69, 0x28,
	0x4C, 0x89, 0x71, 0x30,
	0x4C, 0x89, 0x79, 0x38,
	0x59,
	0x66, 0x0F, 0x38, 0xDC, 0xC4,
	0x66, 0x0F, 0x38, 0xDE, 0xCC,
	0x66, 0x0F, 0x38, 0xDC, 0xD4,
	0x66, 0x0F, 0x38, 0xDE, 0xDC,
	0x66, 0x0F, 0x38, 0xDC, 0xC5,
	0x66, 0x0F, 0x38, 0xDE, 0xCD,
	0x66, 0x0F, 0x38, 0xDC, 0xD5,
	0x66, 0x0F, 0x38, 0xDE, 0xDD,
	0x66, 0x0F, 0x38, 0xDC, 0xC6,
	0x66, 0x0F, 0x38, 0xDE, 0xCE,
	0x66, 0x0F, 0x38, 0xDC, 0xD6,
	0x66, 0x0F, 0x38, 0xDE, 0xDE,
	0x66, 0x0F, 0x38, 0xDC, 0xC7,
	0x66, 0x0F, 0x38, 0xDE, 0xCF,
	0x66, 0x0F, 0x38, 0xDC, 0xD7,
	0x66, 0x0F, 0x38, 0xDE, 0xDF,
	0x66, 0x0F, 0x29, 0x01,
	0x66, 0x0F, 0x29, 0x49, 0x10,
	0x66, 0x0F, 0x29, 0x51, 0x20,
	0x66, 0x0F, 0x29, 0x59, 0x30,
}

/*
#define RANDOMX_SCRATCHPAD_L3 2097152
#define RANDOMX_SCRATCHPAD_MASK      (RANDOMX_SCRATCHPAD_L3-64)
mov rdx, rax
;#and eax, RANDOMX_SCRATCHPAD_MASK
and eax, 2097088
ror rdx, 32
;#and edx, RANDOMX_SCRATCHPAD_MASK
and edx, 2097088
*/
var programCalculateSpAddrs = []byte{
	0x48, 0x89, 0xC2,
	0x25, 0xC0, 0xFF, 0x1F, 0x00,
	0x48, 0xC1, 0xCA, 0x20,
	0x81, 0xE2, 0xC0, 0xFF, 0x1F, 0x00,
}

//go:nosplit
func (f VMProgramFunc) ExecuteFull(rf *RegisterFile, pad *ScratchPad, dataset *RegisterLine, iterations uint64, ma, mx uint32, eMask [2]uint64) {
	if f == nil {
		panic("program is nil")
	}

	jmpPtr := uintptr(unsafe.Pointer(unsafe.SliceData(f)))
	runtime_entersyscall()
	vm_run_full(rf, pad, dataset, iterations, (uint64(ma)<<32)|uint64(mx), eMask, jmpPtr)
	runtime_exitsyscall()

	runtime.KeepAlive(jmpPtr)
	runtime.KeepAlive(f)
	runtime.KeepAlive(rf)
	runtime.KeepAlive(pad)
	runtime.KeepAlive(eMask)
	runtime.KeepAlive(dataset)
	runtime.KeepAlive(iterations)
	runtime.KeepAlive(ma)
	runtime.KeepAlive(mx)
}

//go:nosplit
func (f VMProgramFunc) Execute(rf *RegisterFile, pad *ScratchPad, eMask [2]uint64) {
	if f == nil {
		return
	}

	jmpPtr := uintptr(unsafe.Pointer(unsafe.SliceData(f)))
	vm_run(rf, pad, eMask, jmpPtr)

	runtime.KeepAlive(jmpPtr)
	runtime.KeepAlive(f)
	runtime.KeepAlive(rf)
	runtime.KeepAlive(pad)
	runtime.KeepAlive(eMask)
}

func (c *ByteCode) generateCode(program []byte, v2 bool, readReg *[4]uint64) []byte {
	program = program[:0]

	isFullMode := readReg != nil

	if isFullMode {

		program = append(program, programCalculateSpAddrs...)
		// prologue
		program = append(program, programLoopLoad...)
	}

	var instructionOffsets [RANDOMX_PROGRAM_SIZE]int32

	for ix := range c {
		instructionOffsets[ix] = int32(len(program))

		instr := &c[ix]
		switch instr.Opcode {

		case VM_IADD_RS:
			program = append(program, REX_LEA...)
			if instr.Dst == RegisterNeedsDisplacement {
				program = append(program, 0xac)
			} else {
				program = append(program, 0x04+8*instr.Dst)
			}
			program = append(program, genSIB(int(instr.ImmB), int(instr.Src), int(instr.Dst)))
			if instr.Dst == RegisterNeedsDisplacement {
				program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))
			}

		case VM_IADD_M:
			program = genAddressReg(program, instr, true)
			program = append(program, REX_ADD_RM...)
			program = append(program, 0x04+8*instr.Dst)
			program = append(program, 0x06)
		case VM_IADD_MZ:
			program = append(program, REX_ADD_RM...)
			program = append(program, 0x86+8*instr.Dst)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))

		case VM_ISUB_R:
			program = append(program, REX_SUB_RR...)
			program = append(program, 0xc0+8*instr.Dst+instr.Src)
		case VM_ISUB_I:
			program = append(program, REX_81...)
			program = append(program, 0xe8+instr.Dst)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))

		case VM_ISUB_M:
			program = genAddressReg(program, instr, true)
			program = append(program, REX_SUB_RM...)
			program = append(program, 0x04+8*instr.Dst)
			program = append(program, 0x06)
		case VM_ISUB_MZ:
			program = append(program, REX_SUB_RM...)
			program = append(program, 0x86+8*instr.Dst)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))

		case VM_IMUL_R:
			program = append(program, REX_IMUL_RR...)
			program = append(program, 0xc0+8*instr.Dst+instr.Src)
		case VM_IMUL_I:
			// also handles imul_rcp, with 64-bit special
			if bits.Len64(instr.Imm) > 32 {
				program = append(program, MOV_RAX_I...)
				program = binary.LittleEndian.AppendUint64(program, instr.Imm)
				program = append(program, REX_IMUL_RM...)
				program = append(program, 0xc0+8*instr.Dst)
			} else {
				program = append(program, REX_IMUL_RRI...)
				program = append(program, 0xc0+9*instr.Dst)
				program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))
			}

		case VM_IMUL_M:
			program = genAddressReg(program, instr, true)
			program = append(program, REX_IMUL_RM...)
			program = append(program, 0x04+8*instr.Dst)
			program = append(program, 0x06)
		case VM_IMUL_MZ:
			program = append(program, REX_IMUL_RM...)
			program = append(program, 0x86+8*instr.Dst)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))

		case VM_IMULH_R:
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xc0+instr.Dst)
			program = append(program, REX_MUL_R...)
			program = append(program, 0xe0+instr.Src)
			program = append(program, REX_MOV_R64R...)
			program = append(program, 0xc2+8*instr.Dst)

		case VM_IMULH_M:
			program = genAddressReg(program, instr, false)
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xc0+instr.Dst)
			program = append(program, REX_MUL_MEM...)
			program = append(program, REX_MOV_R64R...)
			program = append(program, 0xc2+8*instr.Dst)
		case VM_IMULH_MZ:
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xc0+instr.Dst)
			program = append(program, REX_MUL_M...)
			program = append(program, 0xa6)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))
			program = append(program, REX_MOV_R64R...)
			program = append(program, 0xc2+8*instr.Dst)

		case VM_ISMULH_R:
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xc0+instr.Dst)
			program = append(program, REX_MUL_R...)
			program = append(program, 0xe8+instr.Src)
			program = append(program, REX_MOV_R64R...)
			program = append(program, 0xc2+8*instr.Dst)

		case VM_ISMULH_M:
			program = genAddressReg(program, instr, false)
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xc0+instr.Dst)
			program = append(program, REX_IMUL_MEM...)
			program = append(program, REX_MOV_R64R...)
			program = append(program, 0xc2+8*instr.Dst)
		case VM_ISMULH_MZ:
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xc0+instr.Dst)
			program = append(program, REX_MUL_M...)
			program = append(program, 0xae)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))
			program = append(program, REX_MOV_R64R...)
			program = append(program, 0xc2+8*instr.Dst)

		case VM_INEG_R:
			program = append(program, REX_NEG...)
			program = append(program, 0xd8+instr.Dst)

		case VM_IXOR_R:
			program = append(program, REX_XOR_RR...)
			program = append(program, 0xc0+8*instr.Dst+instr.Src)
		case VM_IXOR_I:
			program = append(program, REX_XOR_RI...)
			program = append(program, 0xf0+instr.Dst)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))

		case VM_IXOR_M:
			program = genAddressReg(program, instr, true)
			program = append(program, REX_XOR_RM...)
			program = append(program, 0x04+8*instr.Dst)
			program = append(program, 0x06)
		case VM_IXOR_MZ:
			program = append(program, REX_XOR_RM...)
			program = append(program, 0x86+8*instr.Dst)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))

		case VM_IROR_R:
			program = append(program, REX_MOV_RR...)
			program = append(program, 0xc8+instr.Src)
			program = append(program, REX_ROT_CL...)
			program = append(program, 0xc8+instr.Dst)
		case VM_IROR_I:
			program = append(program, REX_ROT_I8...)
			program = append(program, 0xc8+instr.Dst)
			program = append(program, byte(instr.Imm&63))

		case VM_IROL_R:
			program = append(program, REX_MOV_RR...)
			program = append(program, 0xc8+instr.Src)
			program = append(program, REX_ROT_CL...)
			program = append(program, 0xc0+instr.Dst)
		case VM_IROL_I:
			program = append(program, REX_ROT_I8...)
			program = append(program, 0xc0+instr.Dst)
			program = append(program, byte(instr.Imm&63))

		case VM_ISWAP_R:
			program = append(program, REX_XCHG...)
			program = append(program, 0xc0+instr.Src+8*instr.Dst)

		case VM_FSWAP_RF:
			program = append(program, SHUFPD...)
			program = append(program, 0xc0+9*instr.Dst)
			program = append(program, 1)
		case VM_FSWAP_RE:
			program = append(program, SHUFPD...)
			program = append(program, 0xc0+9*(instr.Dst+RegistersCountFloat))
			program = append(program, 1)

		case VM_FADD_R:
			program = append(program, REX_ADDPD...)
			program = append(program, 0xc0+instr.Src+8*instr.Dst)

		case VM_FADD_M:
			program = genAddressReg(program, instr, true)
			program = append(program, REX_CVTDQ2PD_XMM12...)
			program = append(program, REX_ADDPD...)
			program = append(program, 0xc4+8*instr.Dst)

		case VM_FSUB_R:
			program = append(program, REX_SUBPD...)
			program = append(program, 0xc0+instr.Src+8*instr.Dst)

		case VM_FSUB_M:
			program = genAddressReg(program, instr, true)
			program = append(program, REX_CVTDQ2PD_XMM12...)
			program = append(program, REX_SUBPD...)
			program = append(program, 0xc4+8*instr.Dst)

		case VM_FSCAL_R:
			program = append(program, REX_XORPS...)
			program = append(program, 0xc7+8*instr.Dst)

		case VM_FMUL_R:
			program = append(program, REX_MULPD...)
			program = append(program, 0xe0+instr.Src+8*instr.Dst)

		case VM_FDIV_M:
			program = genAddressReg(program, instr, true)
			program = append(program, REX_CVTDQ2PD_XMM12...)
			program = append(program, REX_ANDPS_XMM12...)
			program = append(program, REX_DIVPD...)
			program = append(program, 0xe4+8*instr.Dst)

		case VM_FSQRT_R:
			program = append(program, SQRTPD...)
			program = append(program, 0xe4+9*instr.Dst)

		case VM_CFROUND:
			program = append(program, REX_MOV_RR64...)
			program = append(program, 0xc0+instr.Src)
			rotate := byte((13 - instr.Imm) & 63)
			if rotate != 0 {
				program = append(program, ROL_RAX...)
				program = append(program, rotate)
			}
			if v2 {
				program = append(program, TEST_EAX_60SL13...)
				program = append(program, JNZ_SHORT)
				program = append(program, byte(len(AND_OR_MOV_LDMXCSR)))
			}

			program = append(program, AND_OR_MOV_LDMXCSR...)
		case VM_CBRANCH:
			reg := instr.Dst
			target := instr.jumpTarget() + 1

			jmpOffset := instructionOffsets[target] - (int32(len(program)) + 16)

			if BranchesWithin32B {
				branchBegin := uint32(int32(len(program)) + 7)
				branchEnd := branchBegin
				if jmpOffset >= -128 {
					branchEnd += 9
				} else {
					branchEnd += 13
				}
				// If the jump crosses or touches 32-byte boundary, align it
				if (branchBegin ^ branchEnd) >= 32 {
					alignmentSize := 32 - (branchBegin & 31)
					alignmentSize -= alignmentSize

					program = append(program, JMP_ALIGN_PREFIX[alignmentSize]...)
				}
			}
			program = append(program, REX_ADD_I...)
			program = append(program, 0xc0+reg)
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))

			program = append(program, REX_TEST...)
			program = append(program, 0xc0+reg)
			program = binary.LittleEndian.AppendUint32(program, instr.MemMask)

			if jmpOffset >= -128 {
				program = append(program, JZ_SHORT)
				program = append(program, byte(jmpOffset))
			} else {
				program = append(program, JZ...)
				program = binary.LittleEndian.AppendUint32(program, uint32(jmpOffset-4))
			}

		case VM_ISTORE:
			//genAddressRegDst
			program = append(program, LEA_32...)
			program = append(program, 0x80+instr.Dst)
			if instr.Dst == RegisterNeedsSib {
				program = append(program, 0x24)
			}
			program = binary.LittleEndian.AppendUint32(program, uint32(instr.Imm))
			program = append(program, AND_EAX_I)
			program = binary.LittleEndian.AppendUint32(program, instr.MemMask)

			program = append(program, REX_MOV_MR...)
			program = append(program, 0x04+8*instr.Src)
			program = append(program, 0x06)
		case VM_NOP:
			program = append(program, NOP1...)
		}
	}

	if isFullMode {
		// end of prologue
		program = append(program, REX_MOV_RR...)
		program = append(program, 0xc0+byte(readReg[2]))
		program = append(program, REX_XOR_EAX...)
		program = append(program, 0xc0+byte(readReg[3]))

		// read dataset

		program = append(program, programReadDataset...)

		// epilogue
		program = append(program, REX_MOV_RR64...)
		program = append(program, 0xc0+byte(readReg[0]))
		program = append(program, REX_XOR_RAX_R64...)
		program = append(program, 0xc0+byte(readReg[1]))
		//todo: prefetch scratchpad

		if v2 {
			// V2 Full requires hard aes
			program = append(program, programLoopStoreHardAES...)
		} else {
			program = append(program, programLoopStoreAligned...)
		}

		if BranchesWithin32B {
			branchBegin := uint32(len(program))
			branchEnd := branchBegin + 9

			// If the jump crosses or touches 32-byte boundary, align it
			if (branchBegin ^ branchEnd) >= 32 {
				alignmentSize := 32 - (branchBegin & 31)
				if alignmentSize > 8 {
					program = append(program, NOPX[alignmentSize-9][:alignmentSize-8]...)
					alignmentSize = 8
				}
				program = append(program, NOPX[alignmentSize-1][:alignmentSize]...)
			}
		}

		program = append(program, SUB_EBX...)
		program = append(program, JNZ...)
		program = binary.LittleEndian.AppendUint32(program, uint32(-len(program)-4))
		//exit otherwise

	}

	program = append(program, RET)

	return program
}
