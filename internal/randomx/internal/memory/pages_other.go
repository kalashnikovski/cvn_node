//go:build !(unix || windows) || purego

package memory

var PageNoMemoryErr error

func NewPageAllocator() Allocator {
	return nil
}

func PageReadWrite(memory []byte) error {
	panic("not supported")
}

func PageReadExecute(memory []byte) error {
	panic("not supported")
}

// PageReadWriteExecute Insecure!
func PageReadWriteExecute(memory []byte) error {
	panic("not supported")
}
