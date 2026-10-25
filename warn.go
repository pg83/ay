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
	WarnToolConf:          "tool-conf",
	WarnDupSrc:            "dup-src",
	WarnMissingSource:     "missing-source",
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

	WarnToolConf

	WarnDupSrc

	WarnMissingSource
)

type Warn struct {
	Kind    WarnKind
	Message string
}

type moduleWarnings struct {
	own          []Warn
	dependencies []*moduleWarnings
	reported     bool
}

func (w *moduleWarnings) report(emit func(Warn)) {
	if w == nil || w.reported {
		return
	}

	w.reported = true

	for _, warning := range w.own {
		emit(warning)
	}

	for _, dependency := range w.dependencies {
		dependency.report(emit)
	}
}

type WarnKind int

func (k WarnKind) string() string {
	return warnKindStr[k]
}

func (k WarnKind) String() string {
	return k.string()
}
