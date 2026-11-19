package main

type Tree struct {
	*OsFS
}

func newTree(srcRoot string) *Tree {
	return &Tree{OsFS: newFS(srcRoot)}
}
