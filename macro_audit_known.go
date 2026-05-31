package main

import (
	"embed"
	"regexp"
	"sync"
)

var (
	// goSources embeds every .go file in this package at build time so the
	// runtime audit can mine string literals out of the source without
	// depending on the binary's invocation cwd. Only used when
	// --dump-ignored-macros is on.
	//
	//go:embed *.go
	goSources embed.FS
	// stringLiteralRE matches a Go double-quoted string literal that is
	// composed entirely of `[A-Z0-9_]+` and contains at least one ASCII
	// letter — i.e., the same shape we treat as a "service keyword" macro
	// argument. Trailing/leading whitespace is not allowed because Go
	// string literals don't contain them in our codebase for this idiom.
	stringLiteralRE        = regexp.MustCompile(`"([A-Z][A-Z0-9_]*|[A-Z0-9_]*[A-Z][A-Z0-9_]*)"`)
	knownServiceTokensOnce sync.Once
	knownServiceTokensVal  map[string]struct{}
)

// knownServiceTokens returns the set of uppercase string literals appearing
// in this package's .go sources. Any macro argument that matches the
// service-keyword shape (looksLikeServiceWord, see macro_audit.go) and is
// NOT in this set is unhandled — the macro-specific parser branch does not
// look for it.
func knownServiceTokens() map[string]struct{} {
	knownServiceTokensOnce.Do(func() {
		knownServiceTokensVal = mineServiceTokensFromSources()
	})
	return knownServiceTokensVal
}
func mineServiceTokensFromSources() map[string]struct{} {
	tokens := map[string]struct{}{}
	entries, err := goSources.ReadDir(".")

	if err != nil {
		return tokens
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		data, err := goSources.ReadFile(e.Name())

		if err != nil {
			continue
		}

		for _, match := range stringLiteralRE.FindAllSubmatch(data, -1) {
			tok := string(match[1])

			if looksLikeServiceWord(tok) {
				tokens[tok] = struct{}{}
			}
		}
	}

	return tokens
}
