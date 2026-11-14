package randomx

import "cvn_node/internal/randomx/internal/memory"

type SuperScalarProgramFunc []byte

type VMProgramFunc []byte

func (f SuperScalarProgramFunc) Close() error {
	return memory.FreeSlice(pageAllocator, f)
}

func (f VMProgramFunc) Close() error {
	return memory.FreeSlice(pageAllocator, f)
}
