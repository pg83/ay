package main

import "unsafe"

const (
	intSetMinCap  = 8
	intSetFillNum = 5
	intSetFillDen = 8
)

type IntSet struct {
	keys     []uint64
	bits     []uint64
	mask     uint64
	count    int
	resizeAt int
}

func newIntSet(hint int) *IntSet {
	c := intSetMinCap

	for c*intSetFillNum < hint*intSetFillDen {
		c <<= 1
	}

	s := &IntSet{}

	s.alloc(c)

	return s
}

func (s *IntSet) reset() {
	clear(s.keys)
	clear(s.bits)

	s.count = 0
}

func (s *IntSet) alloc(capacity int) {
	s.keys = make([]uint64, capacity)
	s.bits = make([]uint64, (capacity+63)/64)
	s.mask = uint64(capacity - 1)
	s.resizeAt = capacity * intSetFillNum / intSetFillDen
}

func (s *IntSet) bit(i uint64) bool {
	return s.bits[i>>6]>>(i&63)&1 != 0
}

func (s *IntSet) setBit(i uint64, v bool) {
	b := uint64(1) << (i & 63)

	if v {
		s.bits[i>>6] |= b
	} else {
		s.bits[i>>6] &^= b
	}
}

func (s *IntSet) get(k uint64) (bool, bool) {
	keyData := unsafe.SliceData(s.keys)

	for i := k & s.mask; ; i = (i + 1) & s.mask {
		key := *(*uint64)(unsafe.Add(unsafe.Pointer(keyData), uintptr(i)*unsafe.Sizeof(k)))

		switch key {
		case k:
			return s.bit(i), true
		case 0:
			return false, false
		}
	}
}

func (s *IntSet) put(k uint64, v bool) {
	for {
		i := k & s.mask
		keyData := unsafe.SliceData(s.keys)

		for {
			keyCell := (*uint64)(unsafe.Add(unsafe.Pointer(keyData), uintptr(i)*unsafe.Sizeof(k)))
			ek := *keyCell

			if ek == k {
				s.setBit(i, v)

				return
			}

			if ek == 0 {
				if s.count < s.resizeAt {
					*keyCell = k
					s.setBit(i, v)
					s.count++

					return
				}

				break
			}

			i = (i + 1) & s.mask
		}

		s.grow()
	}
}

func (s *IntSet) grow() {
	oldKeys := s.keys
	oldBits := s.bits

	s.alloc(len(oldKeys) * 2)

	s.count = 0

	for i, k := range oldKeys {
		if k != 0 {
			s.put(k, oldBits[uint64(i)>>6]>>(uint64(i)&63)&1 != 0)
		}
	}
}

func (s *IntSet) len() int {
	return s.count
}
