package main

// These patterns exclude NUL. yyPeekS supplies NUL at EOF, so the ordinary
// mismatch and full-length acceptance paths also handle the end of input.
/*!re2c
	vsp = [\t\n\f\r ];
	vnum = "0" | [1-9] [0-9]*;
	vident = [0-9A-Za-z-];
	vpre = vnum | [0-9]* [A-Za-z-] vident*;
	kchar = [0-9A-Za-z._~!'()*-];
*/

// notsVersionRangeMatch matches the whole constraint as `[>=|=] X.Y.Z` and
// returns the operator and version bounds (os == oe for no operator).
func notsVersionRangeMatch(yyinput string) (os, oe, vs, ve int, ok bool) {
	yycursor, yymarker := 0, 0
	yylimit := len(yyinput)
	/*!stags:re2c format = "var @@ int\n\t_ = @@\n"; */
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:tags = 1;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";
	re2c:define:YYSTAGP = "@@{tag} = yycursor";
	re2c:define:YYSTAGN = "@@{tag} = -1";
	re2c:define:YYSHIFTSTAG = "@@{tag} += @@{shift}";

	vsp* @os (">=" | "=")? @oe vsp* @vs [0-9]+ "." [0-9]+ "." [0-9]+ @ve vsp* { return os, oe, vs, ve, yycursor == yylimit }
	* { return 0, 0, 0, 0, false }
	*/
}

// notsPackageKeyValid reports whether the whole text is an npm package name,
// optionally scoped.
func notsPackageKeyValid(yyinput string) bool {
	yycursor, yymarker := 0, 0
	yylimit := len(yyinput)
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";

	("@" kchar+ "/")? [0-9A-Za-z_~!'()*-] kchar* { return yycursor == yylimit }
	* { return false }
	*/
}

// notsPackageVersionValid reports whether the whole text is a semver
// version with optional pre-release and build parts.
func notsPackageVersionValid(yyinput string) bool {
	yycursor, yymarker := 0, 0
	yylimit := len(yyinput)
	/*!re2c
	re2c:yyfill:enable = 0;
	re2c:define:YYCTYPE = byte;
	re2c:define:YYPEEK = "yyPeekS(yyinput, yycursor)";
	re2c:define:YYSKIP = "yycursor += 1";
	re2c:define:YYBACKUP = "yymarker = yycursor";
	re2c:define:YYRESTORE = "yycursor = yymarker";

	vnum "." vnum "." vnum ("-" vpre ("." vpre)*)? ("+" vident+ ("." vident+)*)? { return yycursor == yylimit }
	* { return false }
	*/
}
