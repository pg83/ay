package main

import "slices"

func knownServiceToken(word string) bool {
	_, ok := slices.BinarySearch(knownServiceTokenList[:], word)

	return ok
}
