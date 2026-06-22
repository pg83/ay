package main

import "testing"

func TestSimdVariantForAVX2(t *testing.T) {
	v, ok := simdVariantFor(tokSrcCAvx2)

	if !ok {
		t.Fatal("simdVariantFor(SRC_C_AVX2) = miss, want hit")
	}

	if v.Suffix != "avx2" {
		t.Fatalf("Suffix = %q, want avx2", v.Suffix)
	}

	if !equalStrings(v.CFlags, []string{"-mavx2", "-mfma", "-mbmi", "-mbmi2"}) {
		t.Fatalf("CFlags = %v", v.CFlags)
	}
}

func TestSimdVariantForAVX512(t *testing.T) {
	v, ok := simdVariantFor(tokSrcCAvx512)

	if !ok {
		t.Fatal("simdVariantFor(SRC_C_AVX512) = miss, want hit")
	}

	if v.Suffix != "avx512" {
		t.Fatalf("Suffix = %q, want avx512", v.Suffix)
	}

	if !equalStrings(v.CFlags, []string{"-mavx512f", "-mavx512cd", "-mavx512bw", "-mavx512dq", "-mavx512vl"}) {
		t.Fatalf("CFlags = %v", v.CFlags)
	}
}

func TestSimdVariantForAMX(t *testing.T) {
	v, ok := simdVariantFor(tokSrcCAmx)

	if !ok {
		t.Fatal("simdVariantFor(SRC_C_AMX) = miss, want hit")
	}

	if v.Suffix != "amx" {
		t.Fatalf("Suffix = %q, want amx", v.Suffix)
	}

	if !equalStrings(v.CFlags, []string{
		"-mamx-tile", "-mamx-int8",
		"-mavx512f", "-mavx512cd", "-mavx512bw", "-mavx512dq", "-mavx512vl",
	}) {
		t.Fatalf("CFlags = %v", v.CFlags)
	}
}
