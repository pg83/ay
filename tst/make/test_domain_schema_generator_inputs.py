from tst.make.lib import MakeTest


class DomainSchemaGeneratorInputsTest(MakeTest):
    def test_python_generator_inputs_include_schema_dependencies(self):
        self.write("ya.make", f"LIBRARY()\nADDINCL(FOR sc {self.project_name})\nRUN_PYTHON3(generate.py IN answer.sc OUT_NOAUTO value.txt)\nRESOURCE(value.txt /value)\nEND()\n")
        self.write("generate.py", "from pathlib import Path\nPath('value.txt').write_text('42')\n")
        self.write("answer.sc", 'include "base.sc";\n')
        self.write("base.sc", "\n")
        node = self.node(self.graph(), "value.txt")
        for name in ("answer.sc", "base.sc"):
            self.assertIn(f"$(S)/{self.project_name}/{name}", node["inputs"])

    def test_a_generated_schema_keeps_the_copy_producer_dependency(self):
        self.write("ya.make", "LIBRARY()\nCOPY_FILE(schema.txt generated.sc)\nSRCS(generated.sc)\nRESOURCE(generated.sc.h /schema)\nEND()\n")
        self.write("schema.txt", "\n")
        graph = self.graph()
        copied = self.node(graph, "generated.sc")
        generated = self.node(graph, "generated.sc.h")
        self.assertIn(copied["uid"], generated["deps"])
