package main

const (
	WarnSysIncl WarnKind = iota

	WarnMissingInclude

	WarnUnsupportedSource

	WarnMissingAddincl

	WarnBucketHash
)

type Warn struct {
	Kind    WarnKind
	Message string
}

type WarnKind int

func (k WarnKind) string() string {
	switch k {
	case WarnSysIncl:
		return "sysincl"
	case WarnMissingInclude:
		return "missing-include"
	case WarnUnsupportedSource:
		return "unsupported-source"
	case WarnMissingAddincl:
		return "missing-addincl"
	case WarnBucketHash:
		return "bucket-hash"
	}

	return "warn"
}

func (k WarnKind) String() string {
	return k.string()
}
