package main

import "unsafe"

type MmapedIntMap[V any] struct {
	keys   []uint64
	values []V
	mask   uint64
}

func (m *MmapedIntMap[V]) get(k uint64) *V {
	mask := m.mask
	keyData := unsafe.Pointer(unsafe.SliceData(m.keys))

	for i := k & mask; ; i = (i + 1) & mask {
		switch *(*uint64)(unsafe.Add(keyData, uintptr(i)*unsafe.Sizeof(k))) {
		case k:
			return unsafeAt(m.values, i)
		case 0:
			return nil
		}
	}
}
