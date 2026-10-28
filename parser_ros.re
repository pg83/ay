package main

/*!re2c
	rsp = [\t\n\f\r ];
	rw = [0-9A-Za-z_];
	rp = [0-9A-Za-z_/];
	rarr = "[" [0-9]* "]";
*/

// rosEnumMatch matches the whole text as `enum NAME [VALUE]` and returns
// the name bounds.
func rosEnumMatch(yyinput string) (ns, ne int, ok bool) {
	yycursor, yymarker := 0, 0
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:eof = 0;
	re2c:tags = 1;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";
	re2c:define:YYLESSTHAN = "yylimit <= yycursor";
	re2c:define:YYSTAGP = "@@{tag} = yycursor";
	re2c:define:YYSTAGN = "@@{tag} = -1";
	re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

	"enum" rsp+ @ns rw+ @ne rsp* rw* { return ns, ne, yycursor == yylimit }
	* { return 0, 0, false }
	$ { return 0, 0, false }
	*/
}

// rosFieldMatch matches the whole text as `[Optional<]TYPE[>][\[N\]] NAME`
// and returns the type bounds.
func rosFieldMatch(yyinput string) (ts, te int, ok bool) {
	yycursor, yymarker := 0, 0
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:eof = 0;
	re2c:tags = 1;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";
	re2c:define:YYLESSTHAN = "yylimit <= yycursor";
	re2c:define:YYSTAGP = "@@{tag} = yycursor";
	re2c:define:YYSTAGN = "@@{tag} = -1";
	re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

	"Optional<" @ts rp+ @te rarr? ">" rarr? rsp+ rw+ { return ts, te, yycursor == yylimit }
	@ts rp+ @te rarr? rsp+ rw+ { return ts, te, yycursor == yylimit }
	* { return 0, 0, false }
	$ { return 0, 0, false }
	*/
}
