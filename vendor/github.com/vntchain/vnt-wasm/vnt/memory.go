package vnt

import (
	"fmt"
)

// Memory implements a simple memory model for the ethereum virtual machine.
type WavmMemory struct {
	Memory []byte
	Pos    int
	Size   map[uint64]int
}

func NewWavmMemory() *WavmMemory {
	return &WavmMemory{
		Size: make(map[uint64]int),
	}
}

// init linear memory with wasm data section
// func (m *WavmMemory) Init(module *wasm.Module) error {
// 	if module.Data != nil {
// 		var index int
// 		for _, v := range module.Data.Entries {
// 			expr, _ := module.ExecInitExpr(v.Offset)
// 			offset, ok := expr.(int32)
// 			if !ok {
// 				return wasm.InvalidValueTypeInitExprError{reflect.Int32, reflect.TypeOf(offset).Kind()}
// 			}
// 			index = int(offset) + len(v.Data)
// 			if bytes.Contains(v.Data, []byte{byte(0)}) {
// 				split := bytes.Split(v.Data, []byte{byte(0)})
// 				var tmpoffset = int(offset)
// 				for _, tmp := range split {
// 					tmplen := len(tmp)
// 					b, res := m.isAddress(tmp)
// 					if b == true {
// 						tmp = common.HexToAddress(string(res)).Bytes()
// 					}
// 					b, res = m.isU256(tmp)
// 					if b == true {
// 						tmp = res
// 					}
// 					fmt.Printf("tmp %s\n", tmp)
// 					m.Set(uint64(tmpoffset), uint64(len(tmp)), tmp)
// 					tmpoffset += tmplen + 1
// 				}
// 			} else {
// 				m.Set(uint64(offset), uint64(len(v.Data)), v.Data)
// 			}
// 		}
// 		m.Pos = index
// 	} else {
// 		m.Pos = 0
// 	}
// 	return nil
// }

// Set sets offset + size to value
func (m *WavmMemory) Set(offset, size uint64, value []byte) {
	// length of Memory may never be less than offset + size.
	// The Memory should be resized PRIOR to setting the memory
	if size > uint64(len(m.Memory)) {
		panic("INVALID memory: Memory empty")
	}

	// It's possible the offset is greater than 0 and size equals 0. This is because
	// the calcMemSize (common.go) could potentially return 0 when size is zero (NO-OP)
	if size > 0 {
		copy(m.Memory[offset:offset+size], value)
		m.Size[offset] = len(value)
		m.Pos = m.Pos + int(size)
	}
}

func (m *WavmMemory) SetBytes(value []byte) (offset int) {
	offset = m.Len()
	m.Set(uint64(offset), uint64(len(value)), value)
	return
}

// Resize resizes the memory to size
func (m *WavmMemory) Resize(size uint64) {
	if uint64(m.Len()) < size {
		m.Memory = append(m.Memory, make([]byte, size-uint64(m.Len()))...)
	}
}

// Get returns offset + size as a new slice
func (m *WavmMemory) Get(offset uint64) (cpy []byte) {
	ptr := uint32(offset)
	if int32(ptr) < 0 {
		ptr = uint32(int32(len(m.Memory)) + int32(ptr))
	}
	offset = uint64(ptr)
	var size int
	var ok bool
	if size, ok = m.Size[offset]; ok {
		if size == 0 {
			return nil
		}
	} else {
		return nil
	}

	if len(m.Memory) > int(offset) {
		cpy = make([]byte, size)
		copy(cpy, m.Memory[offset:offset+uint64(size)])
		return
	}

	return
}

func (m *WavmMemory) NormalizeOffset(offset uint32) uint32 {
	if int32(offset) < 0 {
		offset = uint32(int32(m.MemSize()) + int32(offset))
	}
	return offset
}

// GetPtr returns the offset + size
func (m *WavmMemory) GetPtr(offset uint64) []byte {
	ptr := uint32(offset)
	if int32(ptr) < 0 {
		ptr = uint32(int32(len(m.Memory)) + int32(ptr))
	}
	offset = uint64(ptr)
	var size int
	var ok bool
	if size, ok = m.Size[offset]; ok {
		if size == 0 {
			return nil
		}
	} else {
		return nil
	}
	if len(m.Memory) > int(offset) {
		return m.Memory[offset : offset+uint64(size)]
	}
	return nil
}

// Len returns the length of the backing slice
func (m *WavmMemory) Len() int {
	return m.Pos
}

func (m *WavmMemory) MemSize() int {
	return len(m.Memory)
}

// Data returns the backing slice
func (m *WavmMemory) Data() []byte {
	return m.Memory
}

func (m *WavmMemory) Print() {
	fmt.Printf("### mem %d bytes ###\n", len(m.Memory))
	if len(m.Memory) > 0 {
		addr := 0
		for i := 0; i+32 <= len(m.Memory); i += 32 {
			fmt.Printf("%03d: % x\n", addr, m.Memory[i:i+32])
			addr++
		}
	} else {
		fmt.Println("-- empty --")
	}
	fmt.Println("####################")
}
