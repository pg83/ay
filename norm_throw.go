//go:build aynorm

package main

import "fmt"

type normFailure struct {
	err error
}

func throw(err error) {
	if err != nil {
		panic(normFailure{err})
	}
}

func throw2[T any](val T, err error) T {
	throw(err)

	return val
}

func throwFmt(format string, args ...any) {
	panic(normFailure{fmt.Errorf(format, args...)})
}
