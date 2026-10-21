package main

import (
	"strings"
	"testing"
)

func testThrows(t *testing.T, want string, fn func()) {
	t.Helper()
	err := try(fn)

	if err == nil || !strings.Contains(err.error(), want) {
		t.Fatalf("got %v, want an error containing %q", err, want)
	}
}

func TestSdcMetadataValidation(t *testing.T) {
	for _, tc := range []struct {
		name TOK
		args []string
		want string
	}{
		{tokSdcResourceInfo, []string{"category", "name"}, "category, name and URL"},
		{tokSdcSbResourceInfo, []string{"42"}, "id and name"},
		{tokSdcRosnodeInfo, nil, "one node path"},
		{tokSdcSolibInfo, nil, "one URL"},
		{tokInvalid, nil, "unknown SDC metadata macro"},
	} {
		testThrows(t, tc.want, func() { parseSdcMeta(tc.name, tc.args) })
	}

	testEqual(t, parseSdcMeta(tokSdcSbResourceInfo, []string{"42", "model"}), SdcMetaEntry{
		Name: "model", CopyGlobal: true, Args: []string{"sb-resource", "--sb-id=42", "--name=model", "--unpack=false"},
	})
	testEqual(t, parseSdcMeta(tokSdcRosnodeInfo, []string{"app/node"}), SdcMetaEntry{
		Name: "rosnode", Args: []string{"rosnode", "--node-path=app/node"},
	})
	testEqual(t, parseSdcMeta(tokSdcSolibInfo, []string{"sbr:43"}), SdcMetaEntry{
		Name: "solib", Args: []string{"dynamic-library", "--url=sbr:43"},
	})
}

func TestSdcInstallationKinds(t *testing.T) {
	for kind, want := range map[string]string{
		"COMMON_BIN": "common_bin/file", "COMMON_LIB": "common_lib/file",
		"ETC_ROS": "etc/ros/file", "INSTALL_ROOT": "file",
		"PROFILE_HOOKS": "etc/catkin/profile.d/file", "LAUNCH": "share/robot/sub/file",
		"LIB": "lib/robot/sub/file", "PYTHON": "lib/python3.6/dist-packages/robot/sub/file",
		"TS": "share/launcher/ui/ros_type_gen/output/robot/sub/file",
	} {
		testEqual(t, sdcInstallLocation("robot", kind, "sub/file"), "$(B)/sdc_install/"+want)
	}

	testEqual(t, sdcInstallLocation("robot", "UNKNOWN", "file"), "")
}

func TestSdcInstallFilteringAndSupernodes(t *testing.T) {
	env := newEnvironment()
	d := &ModuleData{moduleStmt: &ModuleStmt{Args: internAnys([]string{"robot"})}}

	env.setString(envSdcInstallKinds, "SHARE COMMON_BIN")
	env.setString(envSdcUseSupernode, "yes")
	applySdcInstall("install", internAnys([]string{
		"LIB", "hidden.so", "package.xml", "TARGET", "hidden",
		"COMMON_BIN", "NODE", "runner", "app", "PROGRAM_STARTER", "source", "starter",
	}), d, env)
	testEqual(t, d.copyFiles, []CopyFileEntry{{Src: "package.xml", Dst: "$(B)/sdc_install/lib/robot/package.xml"}})
	testEqual(t, len(d.bundles), 0)
	testEqual(t, len(d.runPython), 1)
	testEqual(t, anyStrs(d.runPython[0].Args), []string{"runner", "$(B)/sdc_install/common_bin/runner"})
	testEqual(t, len(d.runPrograms), 1)
	testEqual(t, anyStrs(d.runPrograms[0].Args), []string{"source", "$(B)/sdc_install/common_bin/starter"})

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"file"}, "expected an installation kind"},
		{[]string{"COMMON_BIN", "TARGET"}, "missing argument"},
	} {
		testThrows(t, tc.want, func() { applySdcInstall("install", internAnys(tc.args), d, env) })
	}
}

func TestUserverProtoArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"OPTS_FILE"}, "requires a value"},
		{[]string{"PROTODESC", "one", "PROTODESC_NAME", "two"}, "mutually exclusive"},
		{[]string{"OUTPUT_INCLUDES", "file.h"}, "requires FILE"},
		{[]string{"UNKNOWN"}, "unknown argument"},
		{nil, "PROTODESC or PROTODESC_NAME is required"},
	} {
		testThrows(t, tc.want, func() {
			applyUserverProto(UnknownStmt{Name: tokUsrvGenProtoStructs, Args: internAnys(tc.args)}, &ModuleData{})
		})
	}

	d := &ModuleData{}
	applyUserverProto(UnknownStmt{Name: tokUsrvGenProtoStructs, Args: internAnys([]string{
		"PROTODESC_NAME", "all.protodesc", "FILES", "$S/api/a.proto", "api/b.proto", "IMPORTS", "ignored",
	})}, d)
	testEqual(t, d.userverProtos[0].files, []UserverProtoFile{{path: "api/a.proto"}, {path: "api/b.proto"}})
	testEqual(t, d.userverProtos[0].descriptor, "all.protodesc")
}

func TestNotsBuildCommandOverrides(t *testing.T) {
	c := newNotsModule(tokTsVite, newEnvironment())
	apply := func(name string, args ...string) {
		testEqual(t, applyNotsStmt(UnknownStmt{Raw: internStr(name), Args: internAnys(args)}, c), true)
	}

	testEqual(t, c.BuildCommand, "vite build --config vite.config.ts")
	apply("TS_VITE_CONFIG", "one.ts", "two.ts")
	testEqual(t, c.BuildCommand, "vite build --config one.ts && vite build --config two.ts")
	apply("TS_NEXT_BUILD_OPTIONS", "--debug", "--no-lint")
	testEqual(t, c.BuildCommand, "next build --debug --no-lint")
	apply("TS_BUILD_SCRIPT", "custom", "node", "build.js")
	testEqual(t, c.BuildScript, "custom")
	testEqual(t, c.BuildCommand, "node build.js")
	apply("WITH_NODE_MODULES", "PROD")
	testEqual(t, c.Bundle && c.ProductionBundle, true)
	apply("VCS_INFO_FILE", "FILE", "version.json")
	testEqual(t, c.VCSInfo, "version.json")
	apply("TS_OUTPUT_PREFIX")
	testEqual(t, c.HasOutputPrefix, true)
	testEqual(t, c.OutputPrefix, "")
	testEqual(t, applyNotsStmt(UnknownStmt{Raw: internStr("UNKNOWN")}, c), false)
	testThrows(t, "requires a script name", func() { applyNotsStmt(UnknownStmt{Raw: internStr("TS_TEST")}, c) })
}

func TestNotsStableVersionParts(t *testing.T) {
	testEqual(t, notsVersionParts("25.1.2"), []int{25, 1, 2})
	testThrows(t, "invalid stable version", func() { notsVersionParts("25.1") })
	testThrows(t, "invalid syntax", func() { notsVersionParts("25.x.0") })
}
