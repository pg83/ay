package main

// scIncludeMatch matches `include "<target>" ;` at the start of line and
// returns the target bounds.
func scIncludeMatch(yyinput []byte) (ts, te int, ok bool) {
	yycursor, yymarker := 0, 0
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */
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

	sp = [\t\n\v\f\r ];

	"include" sp+ "\"" @ts [^"]+ @te "\"" sp* ";" { return ts, te, true }
	* { return 0, 0, false }
	$ { return 0, 0, false }
	*/
}
