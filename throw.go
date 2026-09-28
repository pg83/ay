package main

import "fmt"

type Exception struct {
	what func() error
}

func (e *Exception) error() string {
	return e.what().Error()
}

func (e *Exception) Error() string {
	return e.error()
}

func (e *Exception) throw() {
	panic(e)
}

func (e *Exception) catch(cb func(*Exception)) {
	if e != nil {
		cb(e)
	}
}

func (e *Exception) asError() error {
	return e.what()
}

func newException(err error) *Exception {
	return &Exception{
		what: func() error {
			return err
		},
	}
}

func exceptionf(format string, args ...any) *Exception {
	return newException(fmt.Errorf(format, args...))
}

func throw(err error) {
	if err != nil {
		newException(err).throw()
	}
}

func throw2[T any](val T, err error) T {
	throw(err)

	return val
}

func throwFmt(format string, args ...any) {
	exceptionf(format, args...).throw()
}

var (
	chaosAssert       = newChaosFault("assert")
	chaosForeignPanic = newChaosFault("foreign-panic")
)

// assert guards an invariant: ok is false only when ay itself is defective.
func assert(ok bool, msg string) {
	if !chaosSwap(chaosAssert, ok, false) {
		throwFmt("%s", msg)
	}
}

func try(cb func()) (err *Exception) {
	defer func() {
		if rec := recover(); rec != nil {
			if exc, ok := rec.(*Exception); ok {
				err = exc
			} else {
				flushCoverage()
				panic(rec)
			}
		}
	}()

	if chaosForeignPanic.fire() {
		panic("chaos: foreign panic")
	}

	cb()

	return nil
}
