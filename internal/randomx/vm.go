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
	"errors"
	"runtime"
	"unsafe"

	"cvn_node/internal/randomx/internal/aes"
	"cvn_node/internal/randomx/internal/memory"
)
import "golang.org/x/crypto/blake2b"

type VM struct {
	flags Flags

	HardAES *aes.HardAES

	Cache   *Cache
	Dataset *Dataset

	State *VMState
}

// VMState This whole struct is aligned to 64 bytes, except after RegisterFile
type VMState struct {
	Pad ScratchPad
	// Buffer first 128 bytes are entropy below rest are program bytes
	Buffer [16*8 + RANDOMX_PROGRAM_SIZE*8]byte

	HashState [blake2b.Size]byte

	RegisterFile RegisterFile

	Program    ByteCode
	JITProgram VMProgramFunc

	// largePages records that this state was allocated in large pages
	// (CVN addition; see README_CVN.md).
	largePages bool
}

func (s *VMState) Close() error {
	if s.JITProgram != nil {
		if err := s.JITProgram.Close(); err != nil {
			return err
		}
	}
	if s.largePages {
		return memory.Free(largePageAllocator, s)
	}
	return memory.Free(cacheLineAlignedAllocator, s)
}

func NewVMState(vm *VM) (*VMState, error) {
	// CVN addition: with RANDOMX_FLAG_LARGE_PAGES the state, which is mostly
	// the 2 MiB scratchpad every hash works in, is placed in large pages when
	// the system grants them, and in ordinary memory otherwise.
	var s *VMState
	var err error
	if vm.flags.Has(RANDOMX_FLAG_LARGE_PAGES) && largePageAllocator != nil {
		if s, err = memory.Allocate[VMState](largePageAllocator); err == nil {
			s.largePages = true
		}
	}
	if s == nil {
		s, err = memory.Allocate[VMState](cacheLineAlignedAllocator)
		if err != nil {
			return nil, err
		}
	}

	if vm.flags.HasJIT() {
		s.JITProgram, err = memory.AllocateSlice[byte](pageAllocator, int(RandomXCodeSize))
		if err != nil {
			return nil, err
		}

		if !vm.flags.Has(RANDOMX_FLAG_SECURE) {
			err = memory.PageReadWriteExecute(s.JITProgram)
			if err != nil {
				s.JITProgram.Close()
				return nil, err
			}
		}
	}

	return s, nil
}

// NewVM  Creates and initializes a RandomX virtual machine.
// *
// * @param flags is any combination of these 5 flags (each flag can be set or not set):
// *        RANDOMX_FLAG_LARGE_PAGES - allocate scratchpad memory in large pages
// *        RANDOMX_FLAG_HARD_AES - virtual machine will use hardware accelerated AES
// *        RANDOMX_FLAG_FULL_MEM - virtual machine will use the full dataset
// *        RANDOMX_FLAG_JIT - virtual machine will use a JIT compiler
// *        RANDOMX_FLAG_SECURE - when combined with RANDOMX_FLAG_JIT, the JIT pages are never
// *                              writable and executable at the same time (W^X policy)
// *        RANDOMX_FLAG_V2 - virtual machine will implement RandomX V2
// *        The numeric values of the first 4 flags are ordered so that a higher value will provide
// *        faster hash calculation and a lower numeric value will provide higher portability.
// *        Using RANDOMX_FLAG_DEFAULT (all flags not set) works on all platforms, but is the slowest.
// * @param cache is a pointer to an initialized randomx_cache structure. Can be
// *        NULL if RANDOMX_FLAG_FULL_MEM is set.
// * @param dataset is a pointer to a randomx_dataset structure. Can be NULL
// *        if RANDOMX_FLAG_FULL_MEM is not set.
// *
// * @return Pointer to an initialized randomx_vm structure.
// *         Returns NULL if:
// *         (1) Scratchpad memory allocation fails.
// *         (2) The requested initialization flags are not supported on the current platform.
// *         (3) cache parameter is NULL and RANDOMX_FLAG_FULL_MEM is not set
// *         (4) dataset parameter is NULL and RANDOMX_FLAG_FULL_MEM is set
// */
func NewVM(flags Flags, cache *Cache, dataset *Dataset) (vm *VM, err error) {
	if cache == nil && !flags.Has(RANDOMX_FLAG_FULL_MEM) {
		return nil, errors.New("nil cache in light mode")
	}
	if dataset == nil && flags.Has(RANDOMX_FLAG_FULL_MEM) {
		return nil, errors.New("nil dataset in full mode")
	}

	vm = &VM{
		Cache:   cache,
		Dataset: dataset,
		flags:   flags,
	}

	vm.State, err = NewVMState(vm)
	if err != nil {
		return nil, err
	}

	if flags.Has(RANDOMX_FLAG_HARD_AES) && aes.HasHardAESImplementation {
		vm.HardAES = aes.NewHardAES()
	}

	return vm, nil
}

//go:nosplit
func xorReg(dst, src *[8 * 8]byte) {
	for i := range dst {
		dst[i] ^= src[i]
	}
}

// run calculate hash based on input. Not thread safe.
// Warning: Underlying callers will run float64 SetRoundingMode directly
// It is the caller's responsibility to set and restore the mode to IEEE 754 roundTiesToEven between full executions
// Additionally, runtime.LockOSThread and defer runtime.UnlockOSThread is recommended to prevent other goroutines sharing these changes
func (vm *VM) run(s *VMState) {

	// buffer first 128 bytes are entropy below rest are program bytes
	if vm.HardAES != nil {
		vm.HardAES.FillAes4Rx4(&s.HashState, s.Buffer[:])
	} else {
		aes.SoftAES{}.FillAes4Rx4(&s.HashState, s.Buffer[:])
	}

	entropy := (*[16]uint64)(unsafe.Pointer(&s.Buffer))

	// do more initialization before we run

	reg := &s.RegisterFile
	reg.Clear()

	// initialize constant registers
	for i := range entropy[:8] {
		reg.A[i/2][i%2] = SmallPositiveFloatBits(entropy[i])
	}

	// memory registers
	var ma, mx uint32

	ma = uint32(entropy[8] & CacheLineAlignMask)
	mx = uint32(entropy[10])

	addressRegisters := entropy[12]

	var readReg [4]uint64
	for i := range readReg {
		readReg[i] = uint64(i*2) + (addressRegisters & 1)
		addressRegisters >>= 1
	}

	datasetOffset := (entropy[13] % (DatasetExtraItems + 1)) * CacheLineSize

	eMask := [2]uint64{ExponentMask(entropy[14]), ExponentMask(entropy[15])}

	prog := s.Buffer[len(entropy)*8:]
	CompileProgramToByteCode(prog, &s.Program)

	var jitProgram VMProgramFunc

	if s.JITProgram != nil {
		if vm.Dataset == nil || (vm.flags.V2() && vm.HardAES == nil) { //light mode or partial full mode
			if vm.flags.Has(RANDOMX_FLAG_SECURE) {
				err := memory.PageReadWrite(s.JITProgram)
				if err != nil {
					panic(err)
				}
				jitProgram = s.Program.generateCode(s.JITProgram, vm.flags.V2(), nil)
				err = memory.PageReadExecute(s.JITProgram)
				if err != nil {
					panic(err)
				}
			} else {
				jitProgram = s.Program.generateCode(s.JITProgram, vm.flags.V2(), nil)
			}
		} else {
			// full mode and we have JIT
			if vm.flags.Has(RANDOMX_FLAG_SECURE) {
				err := memory.PageReadWrite(s.JITProgram)
				if err != nil {
					panic(err)
				}
				jitProgram = s.Program.generateCode(s.JITProgram, vm.flags.V2(), &readReg)
				err = memory.PageReadExecute(s.JITProgram)
				if err != nil {
					panic(err)
				}
			} else {
				jitProgram = s.Program.generateCode(s.JITProgram, vm.flags.V2(), &readReg)
			}

			jitProgram.ExecuteFull(reg, &s.Pad, &vm.Dataset.Memory()[datasetOffset/CacheLineSize], RANDOMX_PROGRAM_ITERATIONS, ma, mx, eMask)
			return
		}
	}

	spAddr0 := uint64(mx)
	spAddr1 := uint64(ma)

	var rlCache RegisterLine
	if (vm.Dataset != nil || (vm.Cache != nil && vm.Cache.hasInitializedJIT() && vm.Cache.flags.HasJIT())) && jitProgram != nil {
		for ic := 0; ic < RANDOMX_PROGRAM_ITERATIONS; ic++ {
			spMix := reg.R[readReg[0]] ^ reg.R[readReg[1]]

			spAddr0 ^= spMix
			spAddr0 &= ScratchpadL3Mask64
			spAddr1 ^= spMix >> 32
			spAddr1 &= ScratchpadL3Mask64

			xorReg(reg.MemoryR(), s.Pad.Load64x8(uint32(spAddr0)))

			for i := uint64(0); i < RegistersCountFloat; i++ {
				reg.F[i] = s.Pad.Load32FA(uint32(spAddr1 + 8*i))
			}

			for i := uint64(0); i < RegistersCountFloat; i++ {
				reg.E[i] = s.Pad.Load32FA(uint32(spAddr1 + 8*(i+RegistersCountFloat)))

				reg.E[i][LOW] = MaskRegisterExponentMantissa(reg.E[i][LOW], eMask[LOW])
				reg.E[i][HIGH] = MaskRegisterExponentMantissa(reg.E[i][HIGH], eMask[HIGH])
			}

			runtime_entersyscall()
			jitProgram.Execute(reg, &s.Pad, eMask)

			mx ^= uint32(reg.R[readReg[2]] ^ reg.R[readReg[3]])
			mx &= uint32(CacheLineAlignMask)

			if vm.Dataset != nil {
				// full mode
				vm.Dataset.prefetchDataset(datasetOffset + uint64(mx))
				// load output from superscalar program to get dataset 64 bytes
				vm.Dataset.readDataset(datasetOffset+uint64(ma), &reg.R)
			} else {
				// light mode
				// execute output from superscalar program to get dataset 64 bytes
				vm.Cache.initDatasetItemJIT(&rlCache, (datasetOffset+uint64(ma))/CacheLineSize)
				for i := range reg.R {
					reg.R[i] ^= rlCache[i]
				}
			}
			runtime_exitsyscall()

			// swap the elements
			mx, ma = ma, mx

			copy(s.Pad[spAddr1:], reg.MemoryR()[:])

			F := reg.MemoryF()
			E := reg.MemoryE()
			if vm.flags.V2() {
				if vm.HardAES != nil {
					vm.HardAES.MergeAes4Rx4(F, E)
				} else {
					aes.SoftAES{}.MergeAes4Rx4(F, E)
				}
			} else {
				xorReg(F, E)
			}
			copy(s.Pad[spAddr0:], F[:])

			spAddr0 = 0
			spAddr1 = 0

		}
	} else {
		for ic := 0; ic < RANDOMX_PROGRAM_ITERATIONS; ic++ {
			spMix := reg.R[readReg[0]] ^ reg.R[readReg[1]]

			spAddr0 ^= spMix
			spAddr0 &= ScratchpadL3Mask64
			spAddr1 ^= spMix >> 32
			spAddr1 &= ScratchpadL3Mask64

			xorReg(reg.MemoryR(), s.Pad.Load64x8(uint32(spAddr0)))

			for i := uint64(0); i < RegistersCountFloat; i++ {
				reg.F[i] = s.Pad.Load32FA(uint32(spAddr1 + 8*i))
			}

			for i := uint64(0); i < RegistersCountFloat; i++ {
				reg.E[i] = s.Pad.Load32FA(uint32(spAddr1 + 8*(i+RegistersCountFloat)))

				reg.E[i][LOW] = MaskRegisterExponentMantissa(reg.E[i][LOW], eMask[LOW])
				reg.E[i][HIGH] = MaskRegisterExponentMantissa(reg.E[i][HIGH], eMask[HIGH])
			}

			s.Program.Execute(reg, &s.Pad, eMask, vm.flags.V2())

			mx ^= uint32(reg.R[readReg[2]] ^ reg.R[readReg[3]])
			mx &= uint32(CacheLineAlignMask)

			if vm.Dataset != nil {
				// full mode
				vm.Dataset.prefetchDataset(datasetOffset + uint64(mx))
				// load output from superscalar program to get dataset 64 bytes
				vm.Dataset.readDataset(datasetOffset+uint64(ma), &reg.R)
			} else {
				// light mode
				// execute output from superscalar program to get dataset 64 bytes
				vm.Cache.initDatasetItem(&rlCache, (datasetOffset+uint64(ma))/CacheLineSize)
				for i := range reg.R {
					reg.R[i] ^= rlCache[i]
				}
			}

			// swap the elements
			mx, ma = ma, mx

			copy(s.Pad[spAddr1:], reg.MemoryR()[:])

			F := reg.MemoryF()
			E := reg.MemoryE()
			if vm.flags.V2() {
				if vm.HardAES != nil {
					vm.HardAES.MergeAes4Rx4(F, E)
				} else {
					aes.SoftAES{}.MergeAes4Rx4(F, E)
				}
			} else {
				xorReg(F, E)
			}
			copy(s.Pad[spAddr0:], F[:])

			spAddr0 = 0
			spAddr1 = 0

		}
	}

}

func (vm *VM) initScratchpad(s *VMState) {
	clear(s.Pad[:])
	if vm.HardAES != nil {
		vm.HardAES.FillAes1Rx4(&s.HashState, s.Pad[:])
	} else {
		aes.SoftAES{}.FillAes1Rx4(&s.HashState, s.Pad[:])
	}
}

func (vm *VM) runLoops(s *VMState) {
	if lockThreadDueToRoundingMode {
		// Lock thread due to rounding mode flags
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	// always force a restore before startup
	ResetRoundingMode(&s.RegisterFile)

	// restore rounding mode at the end
	defer ResetRoundingMode(&s.RegisterFile)

	for chain := 0; chain < RANDOMX_PROGRAM_COUNT-1; chain++ {
		vm.run(s)

		// write R, F, E, A registers
		s.HashState = blake2b.Sum512(s.RegisterFile.Memory()[:])
	}

	// final loop executes here
	vm.run(s)
}

// SetCache Reinitializes a virtual machine with a new Cache.
// This function should be called anytime the Cache is reinitialized with a new key.
// Does nothing if called with a Cache containing the same key value as already set.
// VM must be initialized without RANDOMX_FLAG_FULL_MEM.
func (vm *VM) SetCache(cache *Cache) {
	if vm.flags.Has(RANDOMX_FLAG_FULL_MEM) {
		panic("unsupported")
	}
	vm.Cache = cache
	//todo
}

// SetDataset Reinitializes a virtual machine with a new Dataset.
// VM must be initialized with RANDOMX_FLAG_FULL_MEM.
func (vm *VM) SetDataset(dataset *Dataset) {
	if !vm.flags.Has(RANDOMX_FLAG_FULL_MEM) {
		panic("unsupported")
	}
	vm.Dataset = dataset
}

// CalculateHash Calculates a RandomX hash value.
func (vm *VM) CalculateHash(input []byte, output *[RANDOMX_HASH_SIZE]byte) {
	vm.CalculateHashWithState(vm.State, input, output)
}

// CalculateHashWithState Calculates a RandomX hash value.
func (vm *VM) CalculateHashWithState(s *VMState, input []byte, output *[RANDOMX_HASH_SIZE]byte) {
	s.HashState = blake2b.Sum512(input)

	vm.initScratchpad(s)

	vm.runLoops(s)

	// now hash the scratch pad as it will act as register A
	if vm.HardAES != nil {
		vm.HardAES.HashAes1Rx4(s.Pad[:], &s.HashState)
	} else {
		aes.SoftAES{}.HashAes1Rx4(s.Pad[:], &s.HashState)
	}

	regMem := s.RegisterFile.Memory()
	// write hash onto register A
	copy(regMem[RegisterFileSize-RegistersCountFloat*2*8:], s.HashState[:])

	// write R, F, E, A registers
	*output = blake2b.Sum256(regMem[:])
}

// CalculateHashFirst will begin a hash calculation.
func (vm *VM) CalculateHashFirst(input []byte) {
	s := vm.State
	s.HashState = blake2b.Sum512(input)

	vm.initScratchpad(s)
}

// CalculateHashNext will output the hash value of the previous input and begin the calculation of the next hash.
func (vm *VM) CalculateHashNext(nextInput []byte, output *[RANDOMX_HASH_SIZE]byte) {
	s := vm.State
	vm.runLoops(s)

	// now hash the scratch pad as it will act as register A
	if vm.HardAES != nil {
		vm.HardAES.HashAes1Rx4(s.Pad[:], &s.HashState)
	} else {
		aes.SoftAES{}.HashAes1Rx4(s.Pad[:], &s.HashState)
	}

	// Finish current hash and fill the scratchpad for the next hash at the same time
	regMem := s.RegisterFile.Memory()

	s.HashState = blake2b.Sum512(nextInput)
	// write hash onto register A
	if vm.HardAES != nil {
		vm.HardAES.HashAndFillAes1Rx4(s.Pad[:], (*[64]byte)(unsafe.Pointer(unsafe.SliceData(regMem[RegisterFileSize-RegistersCountFloat*2*8:]))), &s.HashState)
	} else {
		aes.SoftAES{}.HashAndFillAes1Rx4(s.Pad[:], (*[64]byte)(unsafe.Pointer(unsafe.SliceData(regMem[RegisterFileSize-RegistersCountFloat*2*8:]))), &s.HashState)
	}
	runtime.KeepAlive(regMem)

	// write R, F, E, A registers
	*output = blake2b.Sum256(regMem[:])
}

// CalculateHashLast will output the hash value of the previous input.
func (vm *VM) CalculateHashLast(output *[RANDOMX_HASH_SIZE]byte) {
	s := vm.State
	vm.runLoops(s)

	// now hash the scratch pad as it will act as register A
	if vm.HardAES != nil {
		vm.HardAES.HashAes1Rx4(s.Pad[:], &s.HashState)
	} else {
		aes.SoftAES{}.HashAes1Rx4(s.Pad[:], &s.HashState)
	}

	regMem := s.RegisterFile.Memory()
	// write hash onto register A
	copy(regMem[RegisterFileSize-RegistersCountFloat*2*8:], s.HashState[:])

	// write R, F, E, A registers
	*output = blake2b.Sum256(regMem[:])
}

// Close Releases all memory occupied by the structure.
func (vm *VM) Close() error {
	if vm.State != nil {
		vm.State.Close()
		vm.State = nil
	}

	return nil
}
