from tst.make.lib import MakeTest


class YqlResourceOptionsTest(MakeTest):
    def program(self):
        self.write("ya.make", "YQL_PREBUILT_PROGRAM(answer)\nSET(ANSWER_RESOURCE_DEFAULT 123456789)\nSET(ANSWER_RESOURCE_ASAN 123456781)\nSET(ANSWER_RESOURCE_MSAN 123456782)\nSET(ANSWER_RESOURCE_TSAN 123456783)\nSET(ANSWER_RESOURCE_UBSAN 123456784)\nSRCS(main.cpp)\nEND()\n")
        self.write("main.cpp", "int main() { return 0; }\n")

    def test_sanitizers_select_their_matching_prebuilt_resource(self):
        self.program()
        for sanitizer, resource in (("address", "123456781"), ("memory", "123456782"),
                                    ("thread", "123456783"), ("undefined", "123456784")):
            with self.subTest(sanitizer=sanitizer):
                graph = self.graph("-DSANITIZER_TYPE=" + sanitizer)
                self.node(graph, "answer")
                downloaded = self.node(graph, "answer.prebuilt")
                args = [arg for command in downloaded["cmds"] for arg in command["cmd_args"]]
                self.assertTrue(any(resource in arg for arg in args), args)

    def test_source_build_options_disable_the_prebuilt_resource(self):
        self.program()
        for flag in ("-DOPENSOURCE=yes", "-DBUILD_YQL_FROM_SOURCES=yes", "-DSANITIZER_TYPE=leak"):
            with self.subTest(flag=flag):
                graph = self.graph(flag)
                self.node(graph, "main.cpp.o")
                self.assertFalse(any(path.endswith("answer.prebuilt") for node in graph["graph"] for path in node["outputs"]))

    def test_prebuilt_tools_can_omit_a_primary_output(self):
        self.write("ya.make", "YQL_PREBUILT_TOOL(answer)\nEND()\n")
        self.node(self.graph(), "answer")

    def test_opensource_python_udfs_reject_libra_modules(self):
        self.write("ya.make", "YQL_PYTHON_UDF(answer)\nREGISTER_YQL_PYTHON_UDF(ADD_LIBRA_MODULES yes)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "def answer(): return 42\n")
        self.assertNotEqual(self.make("-DOPENSOURCE=yes").returncode, 0)

    def test_internal_python_udfs_can_register_libra_modules(self):
        self.write("ya.make", "YQL_PYTHON_UDF(answer)\nREGISTER_YQL_PYTHON_UDF(ADD_LIBRA_MODULES yes)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "def answer(): return 42\n")
        generated = self.node(self.graph(), "CustomPython.yql_python_udf.cpp")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertEqual(args[-1], "1")

    def test_empty_python_udf_directories_are_rejected(self):
        self.write("ya.make", "YQL_PYTHON_UDF(answer)\nSET(YQL_PYTHON_DIR)\nREGISTER_YQL_PYTHON_UDF()\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "def answer(): return 42\n")
        self.assertNotEqual(self.make().returncode, 0)
