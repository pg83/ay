import unittest

import lib
from test_java_tests import test_nodes


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


class DataValidationTest(unittest.TestCase):
    def test_data_and_build_resources_have_separate_validation_batches(self):
        files = {
            "mod/ya.make": (
                f"LIBRARY()\n{BARE}SET(VALIDATE_DATA yes)\n"
                "DATA(sbr://12=label sbr:/34suffix sbr://12 arcadia/mod/local)\n"
                "FROM_SANDBOX(FILE 56 RENAME RESOURCE OUT_NOAUTO unused.bin)\n"
                "FROM_SANDBOX(FILE 78 RENAME RESOURCE OUT_NOAUTO other.bin)\n"
                "SRCS(a.cpp)\nEND()\n"
            ),
            "mod/a.cpp": "int a;\n",
        }
        graph = lib.make(files, "mod", "--target-platform-tests", "--test-size", "small")
        checks = test_nodes(graph)
        data = checks["mod/validate_data_sbr"]
        resource = checks["mod/validate_resource"]
        self.assertEqual(data["inputs"], ["$(S)/mod", "12", "34"])
        self.assertEqual(resource["inputs"], ["$(S)/mod", "56", "78"])
        for node in (data, resource):
            self.assertEqual(node["requirements"]["network"], "full")
            self.assertIn("check_resource", node["cmds"][-1]["cmd_args"])
            deps = [n["outputs"][0] for n in graph["graph"] if n["uid"] in node["deps"]]
            self.assertEqual(deps, ["$(B)/common_test.context"])
        self.assertFalse(any("unused.bin" in output for node in graph["graph"] for output in node["outputs"]))

    def test_validation_switch_uses_the_final_module_value(self):
        files = {
            "mod/ya.make": (
                f"LIBRARY()\n{BARE}SET(VALIDATE_DATA yes)\n"
                "DATA(sbr://12)\n"
                "DISABLE_DATA_VALIDATION()\n"
                "DATA(sbr://34)\nFROM_SANDBOX(FILE 56 OUT_NOAUTO unused.bin)\n"
                "ENABLE(VALIDATE_DATA)\n"
                "FROM_SANDBOX(FILE 78 OUT_NOAUTO other.bin)\n"
                "SRCS(a.cpp)\nEND()\n"
            ),
            "mod/a.cpp": "int a;\n",
        }
        graph = lib.make(files, "mod", "--target-platform-tests")
        checks = test_nodes(graph)
        self.assertEqual(checks["mod/validate_data_sbr"]["inputs"], ["$(S)/mod", "12", "34"])
        self.assertEqual(checks["mod/validate_resource"]["inputs"], ["$(S)/mod", "56", "78"])
        files["mod/ya.make"] = files["mod/ya.make"].replace("END()", "DISABLE_DATA_VALIDATION()\nEND()")
        self.assertEqual(test_nodes(lib.make(files, "mod", "--target-platform-tests")), {})

    def test_script_modules_get_resource_checks(self):
        files = {"mod/ya.make": (
            "EXECTEST()\nRUN(tool)\nSET(VALIDATE_DATA yes)\nDATA(sbr://12)\n"
            "FROM_SANDBOX(FILE 34 OUT_NOAUTO unused.bin)\nEND()\n"
        )}
        checks = test_nodes(lib.make(files, "mod", "--target-platform-tests"))
        self.assertIn("mod/validate_data_sbr", checks)
        self.assertIn("mod/validate_resource", checks)


if __name__ == "__main__":
    unittest.main(verbosity=2)
