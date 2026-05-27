package main

type Warn struct {
	Kind    WarnKind
	Message string
}

type WarnKind int

const (
	WarnSysIncl WarnKind = iota

	WarnMissingInclude
)

func (k WarnKind) String() string {
	switch k {
	case WarnSysIncl:
		return "sysincl"
	case WarnMissingInclude:
		return "missing-include"
	}
	return "warn"
}
