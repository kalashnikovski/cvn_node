package randomx

import (
	"runtime"
	"unsafe"

	"cvn_node/internal/randomx/internal/argon2"
	"cvn_node/internal/randomx/internal/blake2"
	"cvn_node/internal/randomx/internal/keys"
	"cvn_node/internal/randomx/internal/memory"
)

type MemoryBlock [argon2.BlockSize / 8]uint64

func (m *MemoryBlock) GetLine(addr uint64) *RegisterLine {
	addr >>= 3
	return (*RegisterLine)(unsafe.Pointer(unsafe.SliceData(m[addr : addr+8 : addr+8])))
}

type Cache struct {
	blocks *[RANDOMX_ARGON_MEMORY]MemoryBlock

	programs      [RANDOMX_PROGRAM_COUNT]SuperScalarProgram
	builtPrograms [RANDOMX_PROGRAM_COUNT][]inlineOp

	jitPrograms SuperScalarProgramFunc

	flags Flags
}

// NewCache Creates a randomx_cache structure and allocates memory for RandomX Cache.
// *
// * @param flags is any combination of these 2 flags (each flag can be set or not set):
// *        RANDOMX_FLAG_LARGE_PAGES - allocate memory in large pages
// *        RANDOMX_FLAG_JIT - create cache structure with JIT compilation support; this makes
// *                           subsequent Dataset initialization faster
// *        Optionally, one of these two flags may be selected:
// *        RANDOMX_FLAG_ARGON2_SSSE3 - optimized Argon2 for CPUs with the SSSE3 instruction set
// *                                   makes subsequent cache initialization faster
// *        RANDOMX_FLAG_ARGON2_AVX2 - optimized Argon2 for CPUs with the AVX2 instruction set
// *                                   makes subsequent cache initialization faster
// *
// * @return Pointer to an allocated randomx_cache structure.
// *         Returns NULL if:
// *         (1) memory allocation fails
// *         (2) the RANDOMX_FLAG_JIT is set and JIT compilation is not supported on the current platform
// *         (3) an invalid or unsupported RANDOMX_FLAG_ARGON2 value is set
// */
func NewCache(flags Flags) (c *Cache, err error) {

	var blocks *[RANDOMX_ARGON_MEMORY]MemoryBlock

	if flags.Has(RANDOMX_FLAG_LARGE_PAGES) {
		if largePageAllocator == nil {
			return nil, ErrLargePagesNotSupported
		}
		blocks, err = memory.Allocate[[RANDOMX_ARGON_MEMORY]MemoryBlock](largePageAllocator)
		if err != nil {
			return nil, err
		}
	} else {
		if pageAllocator != nil {
			// this will transparently use transparent pages if needed
			blocks, err = memory.Allocate[[RANDOMX_ARGON_MEMORY]MemoryBlock](pageAllocator)
		} else {
			blocks, err = memory.Allocate[[RANDOMX_ARGON_MEMORY]MemoryBlock](cacheLineAlignedAllocator)
		}

		if err != nil {
			return nil, err
		}
	}

	return &Cache{
		flags:  flags,
		blocks: blocks,
	}, nil
}

//go:nosplit
func (c *Cache) hasInitializedJIT() bool {
	return c.flags.HasJIT() && c.jitPrograms != nil
}

// Close Releases all memory occupied by the Cache structure.
func (c *Cache) Close() error {
	if c.jitPrograms != nil {
		c.jitPrograms.Close()
	}

	if c.flags.Has(RANDOMX_FLAG_LARGE_PAGES) {
		return memory.Free(largePageAllocator, c.blocks)
	} else if pageAllocator != nil {
		return memory.Free(pageAllocator, c.blocks)
	} else {
		return memory.Free(cacheLineAlignedAllocator, c.blocks)
	}
}

// Init Initializes the cache memory and SuperscalarHash using the provided key value.
// Does nothing if called again with the same key value.
func (c *Cache) Init(key []byte) {
	//TODO: cache key and do not regenerate

	argonBlocks := unsafe.Slice((*argon2.Block)(unsafe.Pointer(c.blocks)), len(c.blocks))

	argon2.BuildBlocks(argonBlocks, key, []byte(RANDOMX_ARGON_SALT), RANDOMX_ARGON_ITERATIONS, RANDOMX_ARGON_MEMORY, RANDOMX_ARGON_LANES)

	const nonce uint32 = 0

	gen := blake2.New(key, nonce)

	for i := range c.programs {
		// build a superscalar program
		c.programs[i] = BuildSuperScalarProgram(gen)
	}

	if c.flags.HasJIT() {
		if c.jitPrograms != nil {
			c.jitPrograms.Close()
			c.jitPrograms = nil
		}
		c.jitPrograms = generateSuperscalarCode(c.programs[:])
		if c.jitPrograms == nil {

		} else if err := memory.PageReadExecute(c.jitPrograms); err != nil {

		} else {
			// success
			clear(c.programs[:])
			return
		}
	}
	// fallback if can't compile program
	for i := range c.programs {
		// build a superscalar program
		c.builtPrograms[i] = BuildSuperscalarInline(c.programs[i].Program())
	}

}

const Mask = CacheSize/CacheLineSize - 1

// getMixBlock fetch a 64 byte block in uint64 form
//
//go:nosplit
func (c *Cache) getMixBlock(addr uint64) *RegisterLine {

	addr = (addr & Mask) * CacheLineSize

	block := addr / 1024
	return c.blocks[block].GetLine(addr % 1024)
}

func (c *Cache) GetMemory() *[RANDOMX_ARGON_MEMORY]MemoryBlock {
	return c.blocks
}

func (c *Cache) InitDatasetItem(rl *RegisterLine, itemNumber uint64) {
	if c.hasInitializedJIT() && c.flags.HasJIT() {
		// Lock due to external JIT madness
		runtime.LockOSThread()
		runtime_entersyscall()

		// if we panic here, bad things happen
		c.initDatasetItemJIT(rl, itemNumber)

		runtime_exitsyscall()
		runtime.UnlockOSThread()

		return
	}
	c.initDatasetItem(rl, itemNumber)
}

//go:nosplit
func (c *Cache) initDatasetItemJIT(rl *RegisterLine, itemNumber uint64) {
	init_dataset_superscalar_run(itemNumber, &keys.SuperScalar_Constants, c.GetMemory(), uintptr(unsafe.Pointer(unsafe.SliceData(c.jitPrograms))), rl)
}

func (c *Cache) initDatasetItem(rl *RegisterLine, itemNumber uint64) {
	registerValue := itemNumber

	rl.Init(itemNumber)

	for i := 0; i < RANDOMX_CACHE_ACCESSES; i++ {
		mix := c.getMixBlock(registerValue)

		program := c.programs[i]

		*rl = ExecuteSuperscalarInline(c.builtPrograms[i], *rl)

		for q := range rl {
			rl[q] ^= mix[q]
		}

		registerValue = rl[program.AddressRegister()]

	}
}

func (c *Cache) DatasetInit(dataset []RegisterLine, startItem, endItem uint64) {
	if c.hasInitializedJIT() && c.flags.HasJIT() {
		// Lock due to external JIT madness
		runtime.LockOSThread()
		runtime_entersyscall()

		c.datasetInitJIT(dataset, startItem, endItem)

		runtime_exitsyscall()
		runtime.UnlockOSThread()

		return
	}
	c.datasetInit(dataset, startItem, endItem)
}

//go:nosplit
func (c *Cache) datasetInitJIT(dataset []RegisterLine, startItem, endItem uint64) {
	for itemNumber := startItem; itemNumber < endItem; itemNumber, dataset = itemNumber+1, dataset[1:] {
		c.initDatasetItemJIT(&dataset[0], itemNumber)
	}
}

func (c *Cache) datasetInit(dataset []RegisterLine, startItem, endItem uint64) {
	for itemNumber := startItem; itemNumber < endItem; itemNumber, dataset = itemNumber+1, dataset[1:] {
		c.initDatasetItem(&dataset[0], itemNumber)
	}
}
