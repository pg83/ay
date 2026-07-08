package main

const (
	WarnSysIncl WarnKind = iota

	WarnMissingInclude

	WarnUnsupportedSource

	WarnMissingAddincl

	WarnBucketHash

	WarnUnknownMacro

	WarnBadMacroArgs

	WarnMissingProducer

	WarnModuleFailed
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
	case WarnUnknownMacro:
		return "unknown-macro"
	case WarnBadMacroArgs:
		return "bad-macro-args"
	case WarnMissingProducer:
		return "missing-producer"
	case WarnModuleFailed:
		return "module-failed"
	}

	return "warn"
}

func (k WarnKind) String() string {
	return k.string()
}
