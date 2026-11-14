//go:build (arm64 || amd64 || 386) && !purego

package randomx

import "unsafe"

//go:nosplit
func (pad *ScratchPad) Load32F(addr uint32) (lo, hi float64) {
	a := *(*[2]int32)(unsafe.Pointer(&pad[addr]))
	return float64(a[LOW]), float64(a[HIGH])
}

//go:nosplit
func (pad *ScratchPad) Load32FA(addr uint32) [2]float64 {
	a := *(*[2]int32)(unsafe.Pointer(&pad[addr]))
	return [2]float64{float64(a[LOW]), float64(a[HIGH])}
}
