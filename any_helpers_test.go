package main

// anys builds an []ANY from string literals for CmdArgs test fixtures. Each element
// round-trips through ANY.String() back to the original string, so a fixture built
// with anys serializes identically to the former []string form.
func anys(ss ...string) []ANY { return appendStringAny(nil, ss) }

// testFS is the default mock source tree for platform construction in tests: it
// carries no build/ymake_conf.py, so confCompressesDebug yields false (no -gz=zstd),
// matching a yatool-style conf. testGzFS carries a conf with the -gz=zstd rule
// (ydb-style), used where a test exercises debug-section compression.
var (
	testFS   = newMemFS(nil)
	testGzFS = newMemFS(map[string]string{
		"build/ymake_conf.py": "debug_info_flags.append('-gz=zstd')\n",
	})
)
