from tst.make.lib import MakeTest, PROJECT


class FlatbuffersOptionsTest(MakeTest):
    def test_native_generators_receive_custom_flags(self):
        self.write("ya.make", "FBS_LIBRARY()\nONLY_TAGS(CPP_FBS)\nFLATC_FLAGS(--scoped-enums --gen-name-strings)\nSRCS(answer.fbs answer.fbs64)\nEND()\n")
        for extension in ("fbs", "fbs64"):
            self.write("answer." + extension, "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        graph = self.graph()
        for extension in ("fbs", "fbs64"):
            with self.subTest(extension=extension):
                generated = self.node(graph, "answer." + extension + ".h")
                args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
                for flag in ("--scoped-enums", "--gen-name-strings"):
                    self.assertIn(flag, args)

    def test_generated_64_bit_schemas_keep_their_copy_dependency(self):
        self.write("ya.make", "FBS_LIBRARY()\nONLY_TAGS(CPP_FBS)\nCOPY_FILE(template.fbs64 generated.fbs64)\nSRCS(generated.fbs64)\nEND()\n")
        self.write("template.fbs64", "namespace e2e; table Answer { value:int; } root_type Answer;\n")
        graph = self.graph()
        copied = self.node(graph, "generated.fbs64")
        generated = self.node(graph, "generated.fbs64.h")
        self.assertIn(f"$(B)/{PROJECT}/generated.fbs64", generated["inputs"])
        self.assertIn(copied["uid"], generated["deps"])
