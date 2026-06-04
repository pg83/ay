package main

import (
	"math/rand"
	"testing"
)

var sinkU64 uint64

func TestMortonInterleave(t *testing.T) {
	cases := []struct {
		p, s uint32
		want uint64
	}{
		{0, 0, 0},
		{0, 1, 1},                           // s bit0 -> bit0
		{1, 0, 2},                           // p bit0 -> bit1
		{1, 1, 3},
		{0, 0xFFFFFFFF, 0x5555555555555555}, // all s -> even bits
		{0xFFFFFFFF, 0, 0xAAAAAAAAAAAAAAAA}, // all p -> odd bits
	}

	for _, c := range cases {
		if got := morton(c.p, c.s); got != c.want {
			t.Fatalf("morton(%#x,%#x) = %#x want %#x", c.p, c.s, got, c.want)
		}
	}
}

func TestMortonBijective(t *testing.T) {
	seen := map[uint64][2]uint32{}
	rng := rand.New(rand.NewSource(1))

	for i := 0; i < 300_000; i++ {
		p, s := rng.Uint32(), rng.Uint32()
		k := morton(p, s)

		if prev, ok := seen[k]; ok && prev != [2]uint32{p, s} {
			t.Fatalf("collision: morton%v == morton%v == %#x", prev, [2]uint32{p, s}, k)
		}

		seen[k] = [2]uint32{p, s}
	}
}

func TestMortonMatchesGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(2))

	for i := 0; i < 1_000_000; i++ {
		p, s := rng.Uint32(), rng.Uint32()

		if g, m := mortonGeneric(p, s), morton(p, s); g != m {
			t.Fatalf("morton(%#x,%#x): generic=%#x dispatched=%#x", p, s, g, m)
		}
	}
}

func BenchmarkMorton(b *testing.B) {
	var acc uint64

	for i := 0; i < b.N; i++ {
		acc += morton(uint32(i), uint32(i*2654435761))
	}

	sinkU64 = acc
}

func BenchmarkMortonGeneric(b *testing.B) {
	var acc uint64

	for i := 0; i < b.N; i++ {
		acc += mortonGeneric(uint32(i), uint32(i*2654435761))
	}

	sinkU64 = acc
}
