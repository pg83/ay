package main

// testParserFS is a sentinel FS for parser tests that never trigger
// INCLUDE expansion. Rooted at "/" so Read / Listdir attempts would
// fail loudly rather than silently no-op — but ya.make parse tests
// don't touch FS unless INCLUDE is present.
var testParserFS = NewFS("/")
