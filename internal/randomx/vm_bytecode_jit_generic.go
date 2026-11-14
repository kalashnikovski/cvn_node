//go:build !(windows || unix) || !amd64 || disable_jit || purego

package randomx

func (c *ByteCode) generateCode(program []byte, v2 bool, readReg *[4]uint64) []byte {
	return nil
}

func (f VMProgramFunc) Execute(rf *RegisterFile, pad *ScratchPad, eMask [2]uint64) {

}
func (f VMProgramFunc) ExecuteFull(rf *RegisterFile, pad *ScratchPad, dataset *RegisterLine, iterations uint64, ma, mx uint32, eMask [2]uint64) {

}
