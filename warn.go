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

func (k WarnKind) string() string {
	switch k {
	case WarnSysIncl:
		return "sysincl"
	case WarnMissingInclude:
		return "missing-include"
	}

	return "warn"
}

// String implements fmt.Stringer — the fmt machinery finds it by name;
// internal code calls string().
func (k WarnKind) String() string {
	return k.string()
}
