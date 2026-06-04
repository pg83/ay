//go:build !amd64

package main

func morton(p, s uint32) uint64 { return mortonGeneric(p, s) }
