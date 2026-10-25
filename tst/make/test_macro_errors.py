from tst.make.lib import MakeTest


class MacroErrorsTest(MakeTest):
    def test_incomplete_generator_arguments(self):
        macros = (
            "COPY_FILE(value.txt)",
            "COPY_FILE_WITH_CONTEXT(value.txt)",
            "SET_LIST_PRE_SUF(SOURCES)",
            "DEB_VERSION()",
            "GEN_PCH_FILE(value.h)",
            "GENERATE_IMPLIB(answer)",
            "PROTO_DESCRIPTIONS()",
            "RUN_LUA()",
            "RUN_JAVA_PROGRAM()",
            "RUN_ANTLR4_CPP()",
            "RUN_ANTLR4_CPP_SPLIT()",
            "BPF(answer.c)",
            "BPF_STATIC(answer.c)",
            "LLVM_COMPILE_CXX(answer.cpp)",
            "COMPILE_LUA_21()",
            "BUILDWITH_CYTHON_C()",
            "BUILDWITH_CYTHON_CPP()",
            "CPP_PROTO_PLUGIN()",
            "CPP_PROTO_PLUGIN0()",
            "CPP_PROTO_PLUGIN2()",
            "CPP_EV_PROTO_PLUGIN()",
            "TASKLET_REG()",
            "TASKLET_REG_EXT()",
            "SET_RESOURCE_URI_FROM_JSON(RESOURCE)",
            "YA_TOOLS_CONF()",
            "SDC_DIAGS_SPLIT_GENERATOR_V3()",
            "SDC_DIAGS_SPLIT_GENERATOR_V4()",
            "USRV_GEN_PROTO_STRUCTS(FILE answer.proto)",
            "FROM_ARCHIVE()",
            "FROM_SANDBOX()",
        )
        for macro in macros:
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.write("answer.cpp", "int answer() { return 42; }\n")
                self.assertNotEqual(self.make().returncode, 0, macro)

    def test_python_module_restrictions(self):
        for macro in ("USE_PYTHON2()", "USE_PYTHON3()", "PY_SRCS(answer.py)", "PY_MAIN(answer:main)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"PY_ANY_MODULE(answer)\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.write("answer.cpp", "int answer() { return 42; }\n")
                self.write("answer.py", "def main():\n    print(42)\n")
                self.assertNotEqual(self.make().returncode, 0, macro)

    def test_invalid_ros_arguments(self):
        for macro in ("ROS_DIR()", "ROS_PARTIAL()", "ROS_SRCS(unknown.txt)", "ROS_SRCS(PARTIAL)", "ROS_SRCS(ZERO_COPY Get.srv)"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"ROS_LIBRARY(e2e)\nONLY_TAGS(CPP_ROS)\n{macro}\nEND()\n")
                self.write("Get.srv", "int32 value\n---\nint32 result\n")
                self.assertNotEqual(self.make().returncode, 0, macro)

    def test_invalid_go_generators(self):
        for macro in ("GO_PROTO_PLUGIN(extra)", "GO_OAPI_CODEGEN(types)", "GO_OAPI_CODEGEN_V2(types)", "GO_MOCKGEN_SOURCE()"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"GO_LIBRARY()\nSRCS(answer.go)\n{macro}\nEND()\n")
                self.write("answer.go", "package answer\n")
                self.assertNotEqual(self.make().returncode, 0, macro)

    def test_invalid_declarations(self):
        for description in (
            "LIBRARY()\nLIBRARY(second)\nEND()\n",
            "LIBRARY()\nELSE()\nEND()\n",
            "LIBRARY()\nENDIF()\nEND()\n",
            "LIBRARY()\nSET(A value\nEND()\n",
            'LIBRARY()\nSET(A "unterminated)\nEND()\n',
        ):
            with self.subTest(description=description):
                self.write("ya.make", description)
                self.assertNotEqual(self.make().returncode, 0, description)

    def test_empty_optional_macros(self):
        for macro in ("DECIMAL_MD5_LOWER_32_BITS(answer)", "YMAPS_SPROTO()", "LLVM_BC(answer.cpp)", "COMPILE_NLG()", "IF()\nENDIF()"):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}\nSRCS(answer.cpp)\nEND()\n")
                self.write("answer.cpp", "int answer() { return 42; }\n")
                self.node(self.graph(), "answer.cpp.o")
