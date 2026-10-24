package main

var warnKindStr = [...]string{
	WarnSysIncl:           "sysincl",
	WarnMissingInclude:    "missing-include",
	WarnUnsupportedSource: "unsupported-source",
	WarnMissingAddincl:    "missing-addincl",
	WarnBucketHash:        "bucket-hash",
	WarnUnknownMacro:      "unknown-macro",
	WarnBadMacroArgs:      "bad-macro-args",
	WarnMissingProducer:   "missing-producer",
	WarnModuleFailed:      "module-failed",
	WarnPlugin:            "plugin",
	WarnTestDependency:    "test-dependency",
}

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

	WarnPlugin

	WarnTestDependency
)

type Warn struct {
	Kind    WarnKind
	Message string
}

type WarnKind int

func (k WarnKind) string() string {
	return warnKindStr[k]
}

func (k WarnKind) String() string {
	return k.string()
}
