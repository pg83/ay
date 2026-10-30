import json

from tst.make.lib import MakeTest


class NativeTestModulesTest(MakeTest):
    def test_yt_native_tests_use_the_unittest_runner(self):
        self.write("ya.make", "YT_UNITTEST(answer)\nSRCS(test.cpp)\nEND()\n")
        self.write("test.cpp", '#include <library/cpp/testing/unittest/registar.h>\nY_UNIT_TEST_SUITE(Answer) { Y_UNIT_TEST(Value) { UNIT_ASSERT(true); } }\n')
        self.node(self.graph("-t"), "test-results/unittest/meta.json")

    def test_afl_fuzzing_uses_the_afl_runtime(self):
        self.write("ya.make", "FUZZ(answer)\nSRCS(fuzz.cpp)\nEND()\n")
        self.write("fuzz.cpp", '#include <stddef.h>\n#include <stdint.h>\nextern "C" int LLVMFuzzerTestOneInput(const uint8_t*, size_t) { return 0; }\n')
        binary = self.node(self.graph("-t", "-DUSE_AFL=yes"), "answer")
        self.assertTrue(any("/contrib/libs/afl/" in path for path in binary["inputs"]))

    def test_yql_udf_test_modules_keep_their_tool_environment(self):
        for declaration, tool in (("YQL_UDF_TEST", "YQL_YQLRUN_PATH"), ("YQL_UDF_MINITEST", "YQL_MINIRUN_PATH")):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"{declaration}(answer)\nEND()\n")
                graph = self.graph("-t")
                self.assertIn(tool, json.dumps([node["cmds"] for node in graph["graph"]]))


class BoostTestModulesTest(MakeTest):
    project_name = "contrib/__ay_e2e__"

    def test_boost_test_modules_generate_the_boost_runner(self):
        self.write("test.cpp", '#define BOOST_TEST_MODULE E2E\n#include <boost/test/unit_test.hpp>\nBOOST_AUTO_TEST_CASE(Answer) { BOOST_CHECK_EQUAL(42, 42); }\n')
        for declaration in ("BOOSTTEST", "BOOSTTEST_WITH_MAIN"):
            with self.subTest(declaration=declaration):
                self.write("ya.make", f"{declaration}(answer)\nSRCS(test.cpp)\nEND()\n")
                graph = self.graph("-t")
                self.assertTrue(any("run_boost_test" in json.dumps(node["cmds"]) for node in graph["graph"]))
