/*
Copyright (c) 2019 DERO Foundation. All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice,
this list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
this list of conditions and the following disclaimer in the documentation
and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
may be used to endorse or promote products derived from this software without
specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE
USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package randomx

import (
	"cvn_node/internal/randomx/internal/blake2"
)

type ExecutionPort byte

const (
	Null ExecutionPort = iota
	P0                 = 1
	P1                 = 2
	P5                 = 4
	P01                = P0 | P1
	P05                = P0 | P5
	P015               = P0 | P1 | P5
)

type MacroOP struct {
	Name      string
	Size      int
	Latency   int
	UOP1      ExecutionPort
	UOP2      ExecutionPort
	Dependent bool
}

func (m *MacroOP) GetSize() int {
	return m.Size
}
func (m *MacroOP) GetLatency() int {
	return m.Latency
}
func (m *MacroOP) GetUOP1() ExecutionPort {
	return m.UOP1
}
func (m *MacroOP) GetUOP2() ExecutionPort {
	return m.UOP2
}

func (m *MacroOP) IsSimple() bool {
	return m.UOP2 == Null
}

func (m *MacroOP) IsEliminated() bool {
	return m.UOP1 == Null
}

func (m *MacroOP) IsDependent() bool {
	return m.Dependent
}

// 3 byte instructions
var M_Add_rr = MacroOP{"add r,r", 3, 1, P015, Null, false}
var M_Sub_rr = MacroOP{"sub r,r", 3, 1, P015, Null, false}
var M_Xor_rr = MacroOP{"xor r,r", 3, 1, P015, Null, false}
var M_Imul_r = MacroOP{"imul r", 3, 4, P1, P5, false}
var M_Mul_r = MacroOP{"mul r", 3, 4, P1, P5, false}
var M_Mov_rr = MacroOP{"mov r,r", 3, 0, Null, Null, false}

// latency is 1 lower
var M_Imul_r_dependent = MacroOP{"imul r", 3, 3, P1, Null, true} // this is the dependent version where current instruction depends on previous instruction

// Size: 4 bytes
var M_Lea_SIB = MacroOP{"lea r,r+r*s", 4, 1, P01, Null, false}
var M_Imul_rr = MacroOP{"imul r,r", 4, 3, P1, Null, false}
var M_Ror_ri = MacroOP{"ror r,i", 4, 1, P05, Null, false}

// Size: 7 bytes (can be optionally padded with nop to 8 or 9 bytes)
var M_Add_ri = MacroOP{"add r,i", 7, 1, P015, Null, false}
var M_Xor_ri = MacroOP{"xor r,i", 7, 1, P015, Null, false}

// Size: 10 bytes
var M_Mov_ri64 = MacroOP{"mov rax,i64", 10, 1, P015, Null, false}

// unused are not implemented

type Instruction struct {
	Opcode    byte
	UOP       MacroOP
	SrcOP     int
	ResultOP  int
	DstOP     int
	UOP_Array []MacroOP
}

func (ins *Instruction) GetUOPCount() int {
	if len(ins.UOP_Array) != 0 {
		return len(ins.UOP_Array)
	} else {
		// NOP
		if ins.Opcode == S_NOP { // nop is assumed to be zero bytes
			return 0
		}
		return 1
	}
}

func (ins *Instruction) GetSize() int {

	if len(ins.UOP_Array) != 0 {
		sum_size := 0
		for i := range ins.UOP_Array {
			sum_size += ins.UOP_Array[i].GetSize()
		}
		return sum_size
	} else {
		return ins.UOP.GetSize()
	}
}

func (ins *Instruction) IsSimple() bool {
	if ins.GetSize() == 1 {
		return true
	}
	return false
}

func (ins *Instruction) GetLatency() int {
	if len(ins.UOP_Array) != 0 {
		sum := 0
		for i := range ins.UOP_Array {
			sum += ins.UOP_Array[i].GetLatency()
		}
		return sum
	} else {
		return ins.UOP.GetLatency()
	}
}

const (
	S_INVALID = 0xFF

	S_NOP      = 0xFE
	S_ISUB_R   = 0
	S_IXOR_R   = 1
	S_IADD_RS  = 2
	S_IMUL_R   = 3
	S_IROR_C   = 4
	S_IADD_C7  = 5
	S_IXOR_C7  = 6
	S_IADD_C8  = 7
	S_IXOR_C8  = 8
	S_IADD_C9  = 9
	S_IXOR_C9  = 10
	S_IMULH_R  = 11
	S_ISMULH_R = 12
	S_IMUL_RCP = 13
)

// SrcOP/DstOp are used to selected registers
var ISUB_R = Instruction{Opcode: S_ISUB_R, UOP: M_Sub_rr, SrcOP: 0}
var IXOR_R = Instruction{Opcode: S_IXOR_R, UOP: M_Xor_rr, SrcOP: 0}
var IADD_RS = Instruction{Opcode: S_IADD_RS, UOP: M_Lea_SIB, SrcOP: 0}
var IMUL_R = Instruction{Opcode: S_IMUL_R, UOP: M_Imul_rr, SrcOP: 0}
var IROR_C = Instruction{Opcode: S_IROR_C, UOP: M_Ror_ri, SrcOP: -1}

var IADD_C7 = Instruction{Opcode: S_IADD_C7, UOP: M_Add_ri, SrcOP: -1}
var IXOR_C7 = Instruction{Opcode: S_IXOR_C7, UOP: M_Xor_ri, SrcOP: -1}
var IADD_C8 = Instruction{Opcode: S_IADD_C8, UOP: M_Add_ri, SrcOP: -1}
var IXOR_C8 = Instruction{Opcode: S_IXOR_C8, UOP: M_Xor_ri, SrcOP: -1}
var IADD_C9 = Instruction{Opcode: S_IADD_C9, UOP: M_Add_ri, SrcOP: -1}
var IXOR_C9 = Instruction{Opcode: S_IXOR_C9, UOP: M_Xor_ri, SrcOP: -1}

var IMULH_R = Instruction{Opcode: S_IMULH_R, UOP_Array: []MacroOP{M_Mov_rr, M_Mul_r, M_Mov_rr}, ResultOP: 1, DstOP: 0, SrcOP: 1}
var ISMULH_R = Instruction{Opcode: S_ISMULH_R, UOP_Array: []MacroOP{M_Mov_rr, M_Imul_r, M_Mov_rr}, ResultOP: 1, DstOP: 0, SrcOP: 1}
var IMUL_RCP = Instruction{Opcode: S_IMUL_RCP, UOP_Array: []MacroOP{M_Mov_ri64, M_Imul_r_dependent}, ResultOP: 1, DstOP: 1, SrcOP: -1}

// how random 16 bytes are split into instructions
var buffer0 = []int{4, 8, 4}
var buffer1 = []int{7, 3, 3, 3}
var buffer2 = []int{3, 7, 3, 3}
var buffer3 = []int{4, 9, 3}
var buffer4 = []int{4, 4, 4, 4}
var buffer5 = []int{3, 3, 10}

var decoderToInstructionSize = [][]int{
	buffer0,
	buffer1,
	buffer2,
	buffer3,
	buffer4,
	buffer5,
}

type DecoderType int

const Decoder484 DecoderType = 0
const Decoder7333 DecoderType = 1
const Decoder3733 DecoderType = 2
const Decoder493 DecoderType = 3
const Decoder4444 DecoderType = 4
const Decoder3310 DecoderType = 5

func (d DecoderType) GetSize() int {
	switch d {
	case Decoder484:
		return 3
	case Decoder7333:
		return 4
	case Decoder3733:
		return 4
	case Decoder493:
		return 3
	case Decoder4444:
		return 4
	case Decoder3310:
		return 3

	default:
		panic("unknown decoder")
	}
}
func (d DecoderType) String() string {
	switch d {
	case Decoder484:
		return "Decoder484"
	case Decoder7333:
		return "Decoder7333"
	case Decoder3733:
		return "Decoder3733"
	case Decoder493:
		return "Decoder493"
	case Decoder4444:
		return "Decoder4444"
	case Decoder3310:
		return "Decoder3310"

	default:
		panic("unknown decoder")
	}
}

func FetchNextDecoder(ins *Instruction, cycle int, mulcount int, gen *blake2.Generator) DecoderType {

	if ins.Opcode == S_IMULH_R || ins.Opcode == S_ISMULH_R {
		return Decoder3310
	}

	// make sure multiplication port is satured, if number of multiplications les less than number of cycles, a 4444 is returned
	if mulcount < (cycle + 1) {
		return Decoder4444
	}

	if ins.Opcode == S_IMUL_RCP {
		if gen.GetByte()&1 == 1 {
			return Decoder484
		} else {
			return Decoder493
		}
	}

	// we are here means selecta decoded randomly
	rnd_byte := gen.GetByte()

	switch rnd_byte & 3 {
	case 0:
		return Decoder484
	case 1:
		return Decoder7333
	case 2:
		return Decoder3733
	case 3:
		return Decoder493
	}

	panic("can never reach")
	return Decoder484
}

type SuperScalarProgram []SuperScalarInstruction

func (p SuperScalarProgram) setAddressRegister(addressRegister uint8) {
	p[0].Dst = addressRegister
}

//go:nosplit
func (p SuperScalarProgram) AddressRegister() uint8 {
	return p[0].Dst
}
func (p SuperScalarProgram) Program() []SuperScalarInstruction {
	return p[1:]
}

func BuildSuperScalarProgram(gen *blake2.Generator) SuperScalarProgram {
	cycle := 0
	depcycle := 0
	//retire_cycle := 0
	mulcount := 0
	ports_saturated := false
	program_size := 0
	//current_instruction := INOP
	macro_op_index := 0
	macro_op_count := 0
	throwAwayCount := 0
	code_size := 0
	program := make(SuperScalarProgram, 1, 512)

	var registers [8]Register

	sins := &SuperScalarInstruction{}
	sins.ins = &Instruction{Opcode: S_NOP}

	portbusy := make([][]int, CYCLE_MAP_SIZE)
	for i := range portbusy {
		portbusy[i] = make([]int, 3)
	}

	done := 0

	for decode_cycle := 0; decode_cycle < RANDOMX_SUPERSCALAR_LATENCY && !ports_saturated && program_size < SuperscalarMaxSize; decode_cycle++ {

		decoder := FetchNextDecoder(sins.ins, decode_cycle, mulcount, gen)

		if cycle == 51 {
			//   break
		}

		buffer_index := 0

		for buffer_index < decoder.GetSize() { // generate instructions for the current decoder
			top_cycle := cycle

			if macro_op_index >= sins.ins.GetUOPCount() {
				if ports_saturated || program_size >= SuperscalarMaxSize {
					break
				}
				CreateSuperScalarInstruction(sins, gen, decoderToInstructionSize[decoder][buffer_index], decoder, len(decoderToInstructionSize[decoder]) == (buffer_index+1), buffer_index == 0)
				macro_op_index = 0

			}

			mop := sins.ins.UOP
			if sins.ins.GetUOPCount() == 1 {

			} else {
				mop = sins.ins.UOP_Array[macro_op_index]
			}

			//calculate the earliest cycle when this macro-op (all of its uOPs) can be scheduled for execution
			scheduleCycle := ScheduleMop(&mop, portbusy, cycle, depcycle, false)
			if scheduleCycle < 0 {
				ports_saturated = true
				break
			}

			if macro_op_index == sins.ins.SrcOP { // FIXME
				forward := 0
				for ; forward < LOOK_FORWARD_CYCLES && !sins.SelectSource(scheduleCycle, &registers, gen); forward++ {
					scheduleCycle++
					cycle++
				}

				if forward == LOOK_FORWARD_CYCLES {
					if throwAwayCount < MAX_THROWAWAY_COUNT {
						throwAwayCount++
						macro_op_index = sins.ins.GetUOPCount()
						continue
					}
					break
				}

			}

			if macro_op_index == sins.ins.DstOP { // FIXME
				forward := 0
				for ; forward < LOOK_FORWARD_CYCLES && !sins.SelectDestination(scheduleCycle, throwAwayCount > 0, &registers, gen); forward++ {
					scheduleCycle++
					cycle++
				}

				if forward == LOOK_FORWARD_CYCLES {
					if throwAwayCount < MAX_THROWAWAY_COUNT {
						throwAwayCount++
						macro_op_index = sins.ins.GetUOPCount()
						continue
					}
					break
				}

			}
			throwAwayCount = 0
			// recalculate when the instruction can be scheduled based on operand availability
			scheduleCycle = ScheduleMop(&mop, portbusy, scheduleCycle, scheduleCycle, true)

			depcycle = scheduleCycle + mop.GetLatency() // calculate when will the result be ready

			if macro_op_index == sins.ins.ResultOP { // fix me
				registers[sins.Dst].Latency = depcycle
				registers[sins.Dst].LastOpGroup = sins.OpGroup
				registers[sins.Dst].LastOpPar = sins.OpGroupPar

			}

			code_size += mop.GetSize()
			buffer_index++
			macro_op_index++
			macro_op_count++

			// terminating condition for 99% case
			if scheduleCycle >= RANDOMX_SUPERSCALAR_LATENCY {
				ports_saturated = true
			}
			cycle = top_cycle

			// when all uops of current instruction have been issued, add the instruction to superscalar program
			if macro_op_index >= sins.ins.GetUOPCount() {
				sins.FixSrcReg() // fix src register once and for all
				program = append(program, *sins)

				if sins.ins.Opcode == S_IMUL_R || sins.ins.Opcode == S_IMULH_R || sins.ins.Opcode == S_ISMULH_R || sins.ins.Opcode == S_IMUL_RCP {
					mulcount++
				}

			}

			done++

		}
		cycle++
	}

	var asic_latencies [8]int

	for i := range program {
		if i == 0 {
			continue
		}
		lastdst := asic_latencies[program[i].Dst] + 1
		lastsrc := 0
		if program[i].Dst != program[i].Src {
			lastsrc = asic_latencies[program[i].Src] + 1
		}
		asic_latencies[program[i].Dst] = max(lastdst, lastsrc)
	}

	asic_latency_max := 0
	address_reg := 0

	for i := range asic_latencies {
		if asic_latencies[i] > asic_latency_max {
			asic_latency_max = asic_latencies[i]
			address_reg = i
		}
	}

	// Set AddressRegister hack
	program.setAddressRegister(uint8(address_reg))

	return program
}

const CYCLE_MAP_SIZE int = RANDOMX_SUPERSCALAR_LATENCY + 4
const LOOK_FORWARD_CYCLES int = 4
const MAX_THROWAWAY_COUNT int = 256

// ScheduleUop schedule the uop as early as possible
func ScheduleUop(uop ExecutionPort, portbusy [][]int, cycle int, commit bool) int {
	for ; cycle < CYCLE_MAP_SIZE; cycle++ { // since cycle is value based, its restored on return
		if (uop&P5) != 0 && portbusy[cycle][2] == 0 {
			if commit {
				portbusy[cycle][2] = int(uop)
			}
			return cycle
		}
		if (uop&P0) != 0 && portbusy[cycle][0] == 0 {
			if commit {
				portbusy[cycle][0] = int(uop)
			}
			return cycle
		}
		if (uop&P1) != 0 && portbusy[cycle][1] == 0 {
			if commit {
				portbusy[cycle][1] = int(uop)
			}
			return cycle
		}

	}
	return -1
}

func ScheduleMop(mop *MacroOP, portbusy [][]int, cycle int, depcycle int, commit bool) int {

	if mop.IsDependent() {
		cycle = max(cycle, depcycle)
	}

	if mop.IsEliminated() {
		return cycle
	} else if mop.IsSimple() {
		return ScheduleUop(mop.GetUOP1(), portbusy, cycle, commit)
	} else {
		for ; cycle < CYCLE_MAP_SIZE; cycle++ { // since cycle is value based, its restored on return
			cycle1 := ScheduleUop(mop.GetUOP1(), portbusy, cycle, false)
			cycle2 := ScheduleUop(mop.GetUOP2(), portbusy, cycle, false)

			if cycle1 >= 0 && cycle1 == cycle2 {
				if commit {
					ScheduleUop(mop.GetUOP1(), portbusy, cycle, true)
					ScheduleUop(mop.GetUOP2(), portbusy, cycle, true)
				}
				return cycle1
			}

		}

	}

	return -1
}

type Register struct {
	Value       uint64
	Latency     int
	LastOpGroup int
	LastOpPar   int //-1 = immediate , 0 to 7 register
	Status      int // can be RegisterNeedsDisplacement = 5; //x86 r13 register
	//RegisterNeedsSib = 4; //x86 r12 register
}

// RegisterNeedsDisplacement x86 r13 register
const RegisterNeedsDisplacement = 5

// RegisterNeedsSib x86 r12 register
const RegisterNeedsSib = 4

func (sins *SuperScalarInstruction) SelectSource(cycle int, registers *[8]Register, gen *blake2.Generator) bool {
	availableRegisters := make([]uint8, 0, 8)

	for i := range registers {
		if registers[i].Latency <= cycle {
			availableRegisters = append(availableRegisters, uint8(i))
		}
	}

	if len(availableRegisters) == 2 && sins.Opcode == S_IADD_RS {
		if availableRegisters[0] == RegisterNeedsDisplacement || availableRegisters[1] == RegisterNeedsDisplacement {
			sins.Src = RegisterNeedsDisplacement
			sins.OpGroupPar = int(sins.Src)
			return true
		}
	}

	if selectRegister(availableRegisters, gen, &sins.Src) {

		if sins.GroupParIsSource == 0 {

		} else {
			sins.OpGroupPar = int(sins.Src)
		}
		return true
	}
	return false
}

func (sins *SuperScalarInstruction) SelectDestination(cycle int, allowChainedMul bool, Registers *[8]Register, gen *blake2.Generator) bool {
	var availableRegisters = make([]uint8, 0, 8)

	for i := range Registers {
		if Registers[i].Latency <= cycle && (sins.CanReuse || uint8(i) != sins.Src) &&
			(allowChainedMul || sins.OpGroup != S_IMUL_R || Registers[i].LastOpGroup != S_IMUL_R) &&
			(Registers[i].LastOpGroup != sins.OpGroup || Registers[i].LastOpPar != sins.OpGroupPar) &&
			(sins.Opcode != S_IADD_RS || i != RegisterNeedsDisplacement) {
			availableRegisters = append(availableRegisters, uint8(i))
		}
	}

	return selectRegister(availableRegisters, gen, &sins.Dst)
}

func selectRegister(availableRegisters []uint8, gen *blake2.Generator, reg *uint8) bool {
	index := 0
	if len(availableRegisters) == 0 {
		return false
	}

	if len(availableRegisters) > 1 {
		tmp := gen.GetUint32()

		index = int(tmp % uint32(len(availableRegisters)))
	} else {
		index = 0
	}
	*reg = availableRegisters[index]
	return true
}
