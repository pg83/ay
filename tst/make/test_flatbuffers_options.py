from tst.make.lib import MakeTest, PROJECT


class FlatbuffersOptionsTest(MakeTest):
    def test_native_generator_receives_explicit_flatc_include_directories(self):
        self.write("ya.make", f"FBS_LIBRARY()\nONLY_TAGS(CPP_FBS)\nADDINCL(FOR flatc {PROJECT}/include)\nSRCS(answer.fbs)\nEND()\n")
        self.write("answer.fbs", "namespace e2e; table Answer { value:int; }\n")
        self.write("include/.keep", "")
        generated = self.node(self.graph(), "answer.fbs.h")
        self.assertIn(f"$(S)/{PROJECT}/answer.fbs", generated["inputs"])
        args = [arg.replace("$(SOURCE_ROOT)", "$(S)") for command in generated["cmds"] for arg in command["cmd_args"]]
        self.assertIn(["-I", f"$(S)/{PROJECT}/include"], [args[i:i + 2] for i in range(len(args) - 1)])

    def test_native_and_java_consumers_share_a_64_bit_schema_generator(self):
        self.write("ya.make", "RECURSE(native java)\n")
        self.write("native/ya.make", f"PROGRAM(answer)\nPEERDIR({PROJECT}/schema)\nSRCS(main.cpp)\nEND()\n")
        self.write("native/main.cpp", "int main() { return 0; }\n")
        self.write("java/ya.make", f"JAVA_PROGRAM(answer)\nJDK_VERSION(17)\nJAVA_SRCS(Main.java)\nPEERDIR({PROJECT}/schema)\nEND()\n")
        self.write("java/Main.java", "public class Main {}\n")
        self.write("schema/ya.make", f"FBS_LIBRARY()\nADDINCL({PROJECT}/schema)\nFLATC_FLAGS(--scoped-enums)\nSRCS(answer.fbs answer.fbs64 native.cpp)\nEND()\n")
        for extension in ("fbs", "fbs64"):
            self.write("schema/answer." + extension, "namespace e2e; table Answer { value:int; }\n")
        self.write("schema/native.cpp", "int answer() { return 42; }\n")
        graph = self.graph()
        self.node(graph, "native/answer")
        self.node(graph, "java/answer.jar")
        generated = self.node(graph, "schema/answer.fbs64.h")
        self.assertIn("--scoped-enums", [arg for command in generated["cmds"] for arg in command["cmd_args"]])
        self.assertIn(f"$(S)/{PROJECT}/schema/native.cpp", self.node(graph, "schema/native.cpp.o")["inputs"])

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
