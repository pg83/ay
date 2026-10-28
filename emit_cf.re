package main

import "bytes"

var cfgCmakeDefineKey = []byte("#cmakedefine")

// cfgVarRefNext finds the next `@NAME@` at or after pos and returns the
// name bounds and the offset past the closing @.
func cfgVarRefNext(yyinput []byte, pos int) (ns, ne, end int, ok bool) {
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */

	for {
		i := bytes.IndexByte(yyinput[pos:], '@')

		if i < 0 {
			return 0, 0, 0, false
		}

		yycursor := pos + i + 1
		yymarker := yycursor
		/*!re2c
		re2c:yyfill:enable = 0;
		re2c:eof = 0;
		re2c:tags = 1;
		re2c:define:YYCTYPE = byte;
		re2c:define:YYPEEK = "yyPeek(yyinput, yycursor)";
		re2c:define:YYSKIP = "yycursor += 1";
		re2c:define:YYBACKUP = "yymarker = yycursor";
		re2c:define:YYRESTORE = "yycursor = yymarker";
		re2c:define:YYLESSTHAN = "yylimit <= yycursor";
		re2c:define:YYSTAGP = "@@{tag} = yycursor";
		re2c:define:YYSTAGN = "@@{tag} = -1";
		re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

		cname = [A-Z_] [A-Z0-9_]*;

		@ns cname @ne "@" { return ns, ne, yycursor, true }
		* { pos += i + 1; continue }
		$ { pos += i + 1; continue }
		*/
	}
}

// cfgCmakeDefineNext finds the next `#cmakedefine[01] NAME` at or after pos
// and returns the name bounds and the offset past the name.
func cfgCmakeDefineNext(yyinput []byte, pos int) (ns, ne, end int, ok bool) {
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */

	for {
		i := bytes.Index(yyinput[pos:], cfgCmakeDefineKey)

		if i < 0 {
			return 0, 0, 0, false
		}

		yycursor := pos + i + len(cfgCmakeDefineKey)
		yymarker := yycursor
		/*!re2c
		re2c:yyfill:enable = 0;
		re2c:eof = 0;
		re2c:tags = 1;
		re2c:define:YYCTYPE = byte;
		re2c:define:YYPEEK = "yyPeek(yyinput, yycursor)";
		re2c:define:YYSKIP = "yycursor += 1";
		re2c:define:YYBACKUP = "yymarker = yycursor";
		re2c:define:YYRESTORE = "yycursor = yymarker";
		re2c:define:YYLESSTHAN = "yylimit <= yycursor";
		re2c:define:YYSTAGP = "@@{tag} = yycursor";
		re2c:define:YYSTAGN = "@@{tag} = -1";
		re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

		"01"? [ \t]+ @ns cname @ne { return ns, ne, yycursor, true }
		* { pos += i + 1; continue }
		$ { pos += i + 1; continue }
		*/
	}
}
