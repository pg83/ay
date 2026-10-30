from tst.make.lib import MakeTest, PROJECT


class GeneratedLibraryTemplatesTest(MakeTest):
    def test_template_text_is_not_parsed_as_cpp_by_its_consumer(self):
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/templates)\nRUN_PYTHON3(generate.py IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/templates/generated.in OUT generated.cpp)\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        self.write("templates/ya.make", "GEN_LIBRARY(templates)\nCOPY_FILE_WITH_CONTEXT(template.in generated.in)\nEND()\n")
        self.write("templates/template.in", '#include "dependency.h"\n')
        self.write("templates/dependency.h", "#pragma once\n")
        generated = self.node(self.graph(), "generated.cpp")
        self.assertIn(f"$(B)/{PROJECT}/templates/generated.in", generated["inputs"])
        self.assertIn(f"$(S)/{PROJECT}/templates/template.in", generated["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/templates/dependency.h", generated["inputs"])

    def test_generator_module_input_keeps_its_generated_templates(self):
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/templates)\nRUN_PYTHON3(generate.py IN ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/templates/templates.pkg.fake OUT generated.cpp)\nEND()\n")
        self.write("generate.py", "print('int answer() { return 42; }')\n")
        self.write("templates/ya.make", "GEN_LIBRARY(templates)\nCOPY_FILE(template.in generated.in)\nEND()\n")
        self.write("templates/template.in", "42\n")
        generated = self.node(self.graph(), "generated.cpp")
        self.assertIn(f"$(B)/{PROJECT}/templates/templates.pkg.fake", generated["inputs"])
        self.assertIn(f"$(B)/{PROJECT}/templates/generated.in", generated["inputs"])
