package main

import "strings"

// javaRunProgramRefNext finds the next `${…}` or `$NAME` reference at or
// after pos and returns its bounds.
func javaRunProgramRefNext(yyinput string, pos int) (rs, re int, ok bool) {
	yylimit := len(yyinput)

	for {
		i := strings.IndexByte(yyinput[pos:], '$')

		if i < 0 {
			return 0, 0, false
		}

		yycursor := pos + i
		yymarker := yycursor
		/*!re2c
		re2c:yyfill:enable = 0;
		re2c:eof = 0;
		re2c:define:YYCTYPE = byte;
		re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
		re2c:define:YYSKIP = "yycursor += 1";
		re2c:define:YYBACKUP = "yymarker = yycursor";
		re2c:define:YYRESTORE = "yycursor = yymarker";
		re2c:define:YYLESSTHAN = "yylimit <= yycursor";

		"${" [^${}]+ "}" { return pos + i, yycursor, true }
		"$" [A-Za-z_] [A-Za-z0-9_]* { return pos + i, yycursor, true }
		* { pos += i + 1; continue }
		$ { pos += i + 1; continue }
		*/
	}
}
