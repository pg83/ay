package main

import "testing"

func TestParseProtoImportLineComments(t *testing.T) {
	for _, tc := range []struct {
		line string
		want string
		ok   bool
	}{
		{`message foo { // import "ignored.proto"`, "", false},
		{`import "foo.proto"; // comment`, "foo.proto", true},
		{`import public "foo.proto"; // comment`, "foo.proto", true},
		{`import "foo//bar.proto";`, "", false},
		{`important "foo.proto";`, "", false},
	} {
		got, _, ok := parseProtoImportLine([]byte(tc.line))

		if ok != tc.ok || ok && got.string() != tc.want {
			t.Errorf("parseProtoImportLine(%q) = (%q, %v), want (%q, %v)", tc.line, got.string(), ok, tc.want, tc.ok)
		}
	}
}
