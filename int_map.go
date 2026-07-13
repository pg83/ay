package main

import "unsafe"

const (
	intMapMinCap  = 8
	intMapFillNum = 5
	intMapFillDen = 8
)

type IntMap[V any] struct {
	keys     []uint64
	values   []V
	mask     uint64
	count    int
	resizeAt int
}

func newIntMap[V any](hint int) *IntMap[V] {
	c := intMapMinCap

	for c*intMapFillNum < hint*intMapFillDen {
		c <<= 1
	}

	m := &IntMap[V]{}

	m.alloc(c)

	return m
}

func (m *IntMap[V]) alloc(capacity int) {
	m.keys = make([]uint64, capacity)
	m.values = make([]V, capacity)
	m.mask = uint64(capacity - 1)
	m.resizeAt = capacity * intMapFillNum / intMapFillDen
}

func (m *IntMap[V]) get(k uint64) *V {
	keys := m.keys
	values := m.values
	mask := m.mask
	keyData := unsafe.SliceData(keys)

	for i := k & mask; ; i = (i + 1) & mask {
		key := *(*uint64)(unsafe.Add(unsafe.Pointer(keyData), uintptr(i)*unsafe.Sizeof(k)))

		switch key {
		case k:
			return unsafeAt(values, i)
		case 0:
			return nil
		}
	}
}

func (m *IntMap[V]) cell(k uint64) (*V, bool) {
	for {
		keys := m.keys
		values := m.values
		mask := m.mask
		i := k & mask
		keyData := unsafe.SliceData(keys)

		for {
			keyCell := (*uint64)(unsafe.Add(unsafe.Pointer(keyData), uintptr(i)*unsafe.Sizeof(k)))
			ek := *keyCell

			if ek == k {
				return unsafeAt(values, i), true
			}

			if ek == 0 {
				if m.count < m.resizeAt {
					*keyCell = k
					m.count++

					return unsafeAt(values, i), false
				}

				break
			}

			i = (i + 1) & mask
		}

		m.grow()
	}
}

func (m *IntMap[V]) put(k uint64, v V) {
	cell, _ := m.cell(k)

	*cell = v
}

func (m *IntMap[V]) grow() {
	oldKeys := m.keys
	oldValues := m.values
	count := m.count

	m.alloc(len(oldKeys) * 2)
	keys := m.keys
	values := m.values
	mask := m.mask

	for i, k := range oldKeys {
		if k == 0 {
			continue
		}

		j := k & mask

		for keys[j] != 0 {
			j = (j + 1) & mask
		}

		keys[j] = k
		values[j] = oldValues[i]
	}

	m.count = count
}

func (m *IntMap[V]) len() int {
	return m.count
}
