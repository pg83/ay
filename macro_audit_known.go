package main

import (
	"embed"
	"regexp"
	"sync"
)

var (
	// goSources embeds every .go file so the runtime audit can mine string
	// literals from the source independent of the invocation cwd. Used only
	// when --dump-ignored-macros is on.
	//
	//go:embed *.go
	goSources embed.FS
	// stringLiteralRE matches a Go double-quoted string literal of [A-Z0-9_]
	// with at least one ASCII letter — the same shape we treat as a service
	// keyword macro argument.
	stringLiteralRE        = regexp.MustCompile(`"([A-Z][A-Z0-9_]*|[A-Z0-9_]*[A-Z][A-Z0-9_]*)"`)
	knownServiceTokensOnce sync.Once
	knownServiceTokensVal  map[string]struct{}
)

// knownServiceTokens returns the uppercase string literals in this package's
// .go sources. A macro argument of service-keyword shape (looksLikeServiceWord)
// not in this set is unhandled — no parser branch looks for it.
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
