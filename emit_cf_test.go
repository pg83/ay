package main

import "testing"

func TestBuildCFGVars_BuildTypeFromPlatform(t *testing.T) {
	fs := newMemFS(map[string]string{"m/tmpl.in": "type = @BUILD_TYPE@\n"})

	if got := buildCFGVars(fs, "m/tmpl.in", nil, nil, "RELEASE"); !containsString(got, "BUILD_TYPE=RELEASE") {
		t.Errorf("release-platform CONFIGURE_FILE vars = %v, want BUILD_TYPE=RELEASE", got)
	}

	if got := buildCFGVars(fs, "m/tmpl.in", nil, nil, "DEBUG"); !containsString(got, "BUILD_TYPE=DEBUG") {
		t.Errorf("debug-platform CONFIGURE_FILE vars = %v, want BUILD_TYPE=DEBUG", got)
	}
}
