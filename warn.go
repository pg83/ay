package main

import "fmt"

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
	WarnMisconfiguration:  "misconfiguration",
	WarnBadSrc:            "bad-src",
	WarnUnknownLocation:   "unknown-location",
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

	WarnMisconfiguration

	WarnBadSrc

	WarnUnknownLocation
)

type Warn struct {
	Kind     WarnKind
	Message  string
	includer VFS
	target   ANY
	quoted   bool
}

func (w Warn) text() string {
	if w.includer == 0 {
		return w.Message
	}

	open, close := `<`, `>`

	if w.quoted {
		open, close = `"`, `"`
	}

	return fmt.Sprintf("%s: unresolved include %s%s%s — not found in source, build, search path, or sysincl",
		w.includer.string(), open, w.target.string(), close)
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
