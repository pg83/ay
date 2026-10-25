import json

from tst.make.lib import MakeTest


class MacroBoundariesTest(MakeTest):
    def test_missing_required_arguments(self):
        macros = (
            "PIRE_INLINE_CMD()", "YT_RECORD_DISABLE_PEERDIR(unexpected)", "LLVM_BC(answer.cpp NAME)",
            "LLVM_BC(answer.cpp NAME answer SUFFIX)", "BUNDLE_OUTPUT(tool)", "GO_EMBED_DIR()",
            "TS_PROTO_PACKAGE_NAME()", "SET_CUSTOM_CLANG_TIDY()", "CHECK_CONFIG_H()",
            "DECIMAL_MD5_LOWER_32_BITS()", "DECIMAL_MD5_LOWER_32_BITS(answer.cpp FUNCNAME)",
            "PY_NAMESPACE()", "YQL_LAST_ABI_VERSION(1)", "YQL_CURRENT_ABI_VERSION(1)",
            "YQL_ABI_VERSION(1)", "PROTOC_FATAL_WARNINGS(yes)", "FBS_NAMESPACE()", "PROTO2FBS()",
            "PROTO_NAMESPACE()", "RESOLVE_PROTO(yes)",
            "SRC()", "SRC_C_NO_LTO()", "SRCS_AVX2()", "AR_PLUGIN()", "DYNAMIC_LIBRARY_FROM()",
            "EXPORTS_SCRIPT()", "SELECT_CLANG_SA_CONFIG()", "PY_MAIN()", "PY_CONSTRUCTOR()",
            "TASKLET(extra)", "USE_SA_PLUGINS(plugin)", "USE_UTIL()", "ADDINCL_SELF(FOR cython)", "ADDINCL_SELF(FOR asm)",
            "REGISTER_YQL_PYTHON_UDF(NAME)", "REGISTER_YQL_PYTHON_UDF(NAME Wrong)",
            "REGISTER_YQL_PYTHON_UDF(UNKNOWN value)",
            "USRV_GEN_PROTO_STRUCTS(OPTS_FILE)", "USRV_GEN_PROTO_STRUCTS(OUTPUT_INCLUDES answer.h)",
            "USRV_GEN_PROTO_STRUCTS(PROTODESC one PROTODESC_NAME two)",
            "USRV_GEN_PROTO_STRUCTS(UNEXPECTED)",
        )
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_optional_native_settings(self):
        self.write("answer.cpp", "int answer() { return 42; }\n")
        for macro in ("YMAPS_SPROTO(answer.cpp)", "YT_RECORD_DISABLE_PEERDIR()", "UDF_NO_PROBE()", "YQL_LAST_ABI_VERSION()", "NO_LINT(ktlint)", "DISABLE_DATA_VALIDATION()", "ENABLE(USE_LIGHT_PY2CC)\nDISABLE(USE_LIGHT_PY2CC)", "SET_APPEND(USER_CFLAGS -DE2E=42)", "SET_APPEND(_PROTOC_FLAGS --fatal_warnings)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.node(self.graph(), "answer.cpp.o")

    def test_excluded_build_conditions(self):
        for macro in ("NO_BUILD_IF(STRICT OS_LINUX)", "BUILD_ONLY_IF(STRICT OS_WINDOWS)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.write("answer.cpp", "int answer() { return 42; }\n")
                result = self.make()
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(json.loads(result.stdout)["result"], [])

    def test_unknown_node_builder_outside_multimodule(self):
        self.write("ya.make", "LIBRARY()\nNODE_BUILDER()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)
