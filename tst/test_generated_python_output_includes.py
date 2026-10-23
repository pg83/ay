"""Empty output parsers ignore include hints without dropping generator inputs."""
import unittest

import lib
from test_python2 import python2_base
from test_python_srcs import python_base


def files(kind="run", python2=False, cpp=False):
    result = python2_base() if python2 else python_base()
    lib.tool_program(result, "tools/seed", "seed")
    result["imports/one.py"] = "value = 1\n"
    result["imports/two.py"] = "value = 2\n"
    result["maple/real_input.py"] = "from ignored import module\n"
    declaration = "RUN_PROGRAM(tools/seed IN ${ARCADIA_ROOT}/maple/real_input.py OUT_NOAUTO primary.py second.py" + (" module.cc" if cpp else "") + " OUTPUT_INCLUDES imports/one.py imports/two.py)\n"
    sources = "PY_SRCS(primary.py second.py)\n" + ("SRCS(module.cc)\n" if cpp else "")
    if kind == "copy":
        declaration = "COPY_FILE(real_input.py primary.py OUTPUT_INCLUDES imports/one.py imports/two.py)\n"
        sources = "PY_SRCS(primary.py)\n"
    result["maple/ya.make"] = ("PY2_LIBRARY()\n" if python2 else "PY3_LIBRARY()\n") + lib.BARE_MODULE + declaration + sources + "END()\n"
    return result


class GeneratedPythonOutputIncludesTest(unittest.TestCase):
    def test_python_consumers_ignore_hints_but_keep_actual_generator_inputs(self):
        for python2 in (False, True):
            with self.subTest(python2=python2):
                graph = lib.make(files(python2=python2), "maple")
                producer = lib.node_by_output(graph, "$(B)/maple/primary.py")
                self.assertIn("$(S)/maple/real_input.py", producer["inputs"])
                self.assertEqual(producer["outputs"], ["$(B)/maple/primary.py", "$(B)/maple/second.py"])
                suffix = ".yapyc" if python2 else ".yapyc3"
                for name in ("primary", "second"):
                    bytecode = lib.node_by_output(graph, "$(B)/maple/" + name + ".py" + suffix)
                    self.assertIn(producer["uid"], bytecode["deps"])
                    self.assertIn("$(B)/maple/primary.py", bytecode["inputs"])
                    self.assertNotIn("$(S)/maple/real_input.py", bytecode["inputs"])
                    self.assertFalse(any(path.startswith("$(S)/imports/") for path in bytecode["inputs"]))
                resources = [n for n in graph["graph"] if any("resfs/src/" in arg and "primary.py" in arg for c in n["cmds"] for arg in c["cmd_args"])]
                self.assertTrue(resources)
                for resource in resources:
                    self.assertFalse(any(path.startswith("$(S)/imports/") for path in resource["inputs"]))

    def test_empty_parser_policy_also_applies_to_copy_producers(self):
        graph = lib.make(files("copy"), "maple")
        producer = lib.node_by_output(graph, "$(B)/maple/primary.py")
        self.assertIn("$(S)/maple/real_input.py", producer["inputs"])
        bytecode = lib.node_by_output(graph, "$(B)/maple/primary.py.yapyc3")
        self.assertIn(producer["uid"], bytecode["deps"])
        self.assertFalse(any(path.startswith("$(S)/imports/") for path in bytecode["inputs"]))

    def test_mixed_cpp_output_keeps_its_declared_include_dependencies(self):
        graph = lib.make(files(cpp=True), "maple")
        cc = lib.node_by_output(graph, "$(B)/maple/module.cc.o")
        for name in ("one", "two"):
            self.assertIn("$(S)/imports/" + name + ".py", cc["inputs"])
        py = lib.node_by_output(graph, "$(B)/maple/primary.py.yapyc3")
        self.assertFalse(any(path.startswith("$(S)/imports/") for path in py["inputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
