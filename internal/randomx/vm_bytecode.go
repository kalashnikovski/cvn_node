package randomx

type ByteCodeInstruction struct {
	Dst, Src byte
	ImmB     uint8
	Opcode   ByteCodeInstructionOp
	MemMask  uint32
	Imm      uint64
	EMask    uint64
	/*
		union {
			int_reg_t* idst;
			rx_vec_f128* fdst;
		};
		union {
			int_reg_t* isrc;
			rx_vec_f128* fsrc;
		};
		union {
			uint64_t imm;
			int64_t simm;
		};
		InstructionType type;
		union {
			int16_t target;
			uint16_t shift;
		};
		uint32_t memMask;
	*/

}

func (i ByteCodeInstruction) jumpTarget() int {
	return int(int16((uint16(i.ImmB) << 8) | uint16(i.Src)))
}

func (i ByteCodeInstruction) getScratchpadAddress(ptr uint64) uint32 {
	return uint32(ptr+i.Imm) & i.MemMask
}

func (i ByteCodeInstruction) getScratchpadZeroAddress() uint32 {
	return uint32(i.Imm) & i.MemMask
}

type ByteCode [RANDOMX_PROGRAM_SIZE]ByteCodeInstruction

type ByteCodeInstructionOp int

const (
	VM_NOP = ByteCodeInstructionOp(iota)
	VM_IADD_RS
	VM_IADD_M
	VM_IADD_MZ
	VM_ISUB_R
	VM_ISUB_I
	VM_ISUB_M
	VM_ISUB_MZ
	VM_IMUL_R
	VM_IMUL_I
	VM_IMUL_M
	VM_IMUL_MZ
	VM_IMULH_R
	VM_IMULH_M
	VM_IMULH_MZ
	VM_ISMULH_R
	VM_ISMULH_M
	VM_ISMULH_MZ
	VM_INEG_R
	VM_IXOR_R
	VM_IXOR_I
	VM_IXOR_M
	VM_IXOR_MZ
	VM_IROR_R
	VM_IROR_I
	VM_IROL_R
	VM_IROL_I
	VM_ISWAP_R
	VM_FSWAP_RF
	VM_FSWAP_RE
	VM_FADD_R
	VM_FADD_M
	VM_FSUB_R
	VM_FSUB_M
	VM_FSCAL_R
	VM_FMUL_R
	VM_FDIV_M
	VM_FSQRT_R
	VM_CFROUND
	VM_CBRANCH
	VM_ISTORE
)
