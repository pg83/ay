import json

from tst.make.lib import MakeTest, PROJECT, nodejs_version


class MiscConfigureErrorsTest(MakeTest):
    """Configure errors of devtools/ymake (add_iter.cpp, module_builder.cpp, module_confs.cpp, module_dir.cpp,
    main.cpp, makefile_loader.cpp) and of ytest.py, gobuild.py and _dart_fields.py not covered elsewhere."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\nSRCS(value.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("value.cpp", "int value() { return 42; }\n")

    def unittest(self, *statements):
        self.write("ya.make", "UNITTEST()\nSRCS(test.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")

    def test_peerdir_to_a_multimodule_excluding_the_tag(self):
        self.write("ya.make", f"GO_LIBRARY()\nSRCS(value.go)\nPEERDIR({PROJECT}/proto)\nEND()\n")
        self.write("value.go", "package value\n")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(value.proto)\nEXCLUDE_TAGS(GO_PROTO)\nEND()\n")
        self.write("proto/value.proto", "syntax = \"proto3\";\nmessage Value {}\n")
        self.assertConfigureLikeYa()

    def test_srcs_with_a_directory(self):
        self.library("SRCS(sub)")
        self.write("sub/value.cpp", "int sub() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_dll_with_exports_in_the_header(self):
        self.write("ya.make", "DLL(answer EXPORTS answer.exports)\nSRCS(value.cpp)\nEND()\n")
        self.write("value.cpp", "int value() { return 42; }\n")
        self.write("answer.exports", "C value\n")
        self.assertConfigureLikeYa()

    def test_depends_on_a_directory_without_modules(self):
        self.unittest(f"DEPENDS({PROJECT}/sub)")
        self.write("sub/ya.make", "RECURSE()\n")
        self.assertConfigureLikeYa()

    def test_depends_on_a_multimodule_without_final_targets(self):
        self.unittest(f"DEPENDS({PROJECT}/proto)")
        self.write("proto/ya.make", "PROTO_LIBRARY()\nSRCS(value.proto)\nEND()\n")
        self.write("proto/value.proto", "syntax = \"proto3\";\nmessage Value {}\n")
        self.assertConfigureLikeYa()

    def test_autoinclude_without_linters_make_inc(self):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nAUTOINCLUDE_PATHS+={PROJECT}/autoincludes.json\n")
        self.write("autoincludes.json", json.dumps([PROJECT]))
        self.library()
        self.assertConfigureLikeYa()

    def test_jtest_for_without_arguments(self):
        self.write("ya.make", "JTEST_FOR()\nJDK_VERSION(17)\nEND()\n")
        self.assertConfigureLikeYa()

    def test_go_resource_key_without_a_value(self):
        self.write("ya.make", "GO_LIBRARY()\nSRCS(value.go)\nRESOURCE(- E2E)\nEND()\n")
        self.write("value.go", "package value\n")
        self.assertConfigureLikeYa()

    def ts_tests(self, *statements):
        self.write("lib/ya.make", "TS_LIBRARY()\nTS_BUILD_SCRIPT(nots:build)\nTS_BUILD_OUTPUTS(dist)\n" + "".join(s + "\n" for s in statements) + "END()\n")
        self.write("lib/package.json", json.dumps({"name": "@e2e/lib", "version": "1.0.0", "engines": {"node": nodejs_version()}, "files": ["src", "dist"], "scripts": {"nots:build": "tsc"}}))
        self.write("lib/pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n")
        self.write("lib/tsconfig.json", json.dumps({"compilerOptions": {"rootDir": "src", "outDir": "dist"}, "include": ["src/**/*.ts"]}))
        self.write("lib/src/answer.ts", "export const answer = 42;\n")
        self.write("lib/src/answer.css", "a { color: red; }\n")
        self.write("ya.make", "RECURSE(lib)\n")

    def test_stylelint_with_a_missing_config(self):
        self.ts_tests("TS_STYLELINT(missing.json)")
        self.assertConfigureLikeYa()

    def test_biome_with_a_missing_config(self):
        self.ts_tests("TS_BIOME(missing.json)")
        self.assertConfigureLikeYa()


class ContribVersionConfigureErrorsTest(MakeTest):
    """Explicit VERSION for modules in contrib (devtools/ymake/general_parser.cpp)."""

    project_name = "contrib/__ay_e2e__"

    def test_contrib_library_without_a_version(self):
        self.write("ya.make", "LIBRARY()\nLICENSE(MIT)\nSRCS(value.cpp)\nEND()\n")
        self.write("value.cpp", "int value() { return 42; }\n")
        self.assertConfigureLikeYa()
