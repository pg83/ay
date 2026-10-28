package main

func yyPeek(input []byte, at int) byte {
	if at < len(input) {
		return input[at]
	}

	return 0
}

func yyPeekS(input string, at int) byte {
	if at < len(input) {
		return input[at]
	}

	return 0
}
