package main

type Warn struct {
	Kind    WarnKind
	Message string
}

type WarnKind int

const (
	WarnSysIncl WarnKind = iota

	WarnMissingInclude

	WarnUnsupportedSource
)

func (k WarnKind) string() string {
	switch k {
	case WarnSysIncl:
		return "sysincl"
	case WarnMissingInclude:
		return "missing-include"
	case WarnUnsupportedSource:
		return "unsupported-source"
	}

	return "warn"
}

// String implements fmt.Stringer; internal code calls string().
func (k WarnKind) String() string {
	return k.string()
}
