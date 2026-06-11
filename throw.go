package main

import "fmt"

type Exception struct {
	what func() error
}

func (e *Exception) error() string {
	return e.what().Error()
}

// Error implements the error interface; internal code calls error().
func (e *Exception) Error() string {
	return e.error()
}

func (e *Exception) unwrap() error {
	return e.what()
}

// Unwrap implements the errors.Is/As unwrap chain.
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

func New(err error) *Exception {
	return &Exception{
		what: func() error {
			return err
		},
	}
}

func Fmt(format string, args ...any) *Exception {
	return New(fmt.Errorf(format, args...))
}

func Throw(err error) {
	if err != nil {
		New(err).throw()
	}
}

func Throw2[T any](val T, err error) T {
	Throw(err)

	return val
}

func ThrowFmt(format string, args ...any) {
	Fmt(format, args...).throw()
}

type HTTPError struct {
	Status int
	Msg    string
}

func Try(cb func()) (err *Exception) {
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
