package main

import (
	"fmt"
	"strings"
)

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
	WarnSyntax:            "syntax",
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

	WarnSyntax
)

type Warn struct {
	Kind     WarnKind
	Message  string
	includer VFS
	target   ANY
	quoted   bool

	Where  string
	Error  string
	Row    int
	Column int
	ts     bool

	includeText string
}

func makeFileWhere(dir string) string {
	return "$S/" + dir + "/ya.make"
}

func (w Warn) text() string {
	if w.Message == "" && w.Error != "" {
		return stripMarkup(w.Error)
	}

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

func stripMarkup(s string) string {
	var b strings.Builder

	for {
		i := strings.Index(s, "[[")

		if i < 0 {
			b.WriteString(s)

			return b.String()
		}

		j := strings.Index(s[i:], "]]")

		if j < 0 {
			b.WriteString(s)

			return b.String()
		}

		b.WriteString(s[:i])
		s = s[i+j+2:]
	}
}
