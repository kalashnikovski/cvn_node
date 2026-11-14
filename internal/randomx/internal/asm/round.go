package asm

func SetRoundingMode[T ~uint64 | ~uint8](mode T) {
	setRoundingMode(uint8(mode))
}
