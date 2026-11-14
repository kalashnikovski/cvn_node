//go:build !(windows || unix) || !amd64 || purego || disable_jit

package randomx

func init_dataset_superscalar_run(itemNumber uint64, constants *[8]uint64, memory *[RANDOMX_ARGON_MEMORY]MemoryBlock, programs uintptr, rl *RegisterLine) {

}

// generateSuperscalarCode
func generateSuperscalarCode(scalarProgram []SuperScalarProgram) SuperScalarProgramFunc {
	return nil
}
