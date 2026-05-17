package main

// Test-only shims for parser-layer helpers that were removed from
// production IncludeScanner. Tests still probe parser behaviour through
// a scanner fixture because it already wires sourceRoot/sysincl setup.

func (s *IncludeScanner) scanDirectives(vfsPath VFS) []includeDirective {
	return s.parsers.scanDirectives(vfsPath)
}

func (s *IncludeScanner) parsedIncludes(vfsPath VFS) parsedIncludeSet {
	return s.parsers.sourceParsedBuckets(vfsPath)
}

func (s *IncludeScanner) sourceParsedBuckets(vfsPath VFS) parsedIncludeSet {
	return s.parsers.sourceParsedBuckets(vfsPath)
}
