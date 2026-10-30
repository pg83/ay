from tst.make.lib import MakeTest


class PythonFlatbufferNativeMacrosTest(MakeTest):
    def test_python_flatbuffer_variants_ignore_native_enum_generators(self):
        self.write("ya.make", "FBS_LIBRARY()\nONLY_TAGS(PY3_FBS)\nGENERATE_ENUM_SERIALIZATION(answer.h)\nSRCS(answer.fbs)\nEND()\n")
        self.write("answer.h", "#pragma once\nenum class Answer { Value = 42 };\n")
        self.write("answer.fbs", "namespace e2e; table Value { answer:int; } root_type Value;\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{self.project_name}/answer.fbs" in node["inputs"] and any(output.endswith(".fbs.pysrc") for output in node["outputs"]) for node in graph["graph"]))
        self.assertFalse(any(output.endswith("_serialized.cpp") for node in graph["graph"] for output in node["outputs"]))
