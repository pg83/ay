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

func (e *Exception) unwrap() error {
	return e.what()
}

func (e *Exception) Unwrap() error {
	return e.unwrap()
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
	if e == nil {
		return nil
	}

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

func try(cb func()) (err *Exception) {
	defer func() {
		if rec := recover(); rec != nil {
			if exc, ok := rec.(*Exception); ok {
				err = exc
			} else {
				panic(rec)
			}
		}
	}()

	cb()

	return nil
}
