from tst.make.lib import MakeTest


class PremoduleProducerDeclarationsTest(MakeTest):
    def test_ragel_producers_require_a_module(self):
        self.write("ya.make", "BUILDWITH_RAGEL6(lexer.rl)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("lexer.rl", "%%{ machine scanner; main := any*; }%%\n%% write data;\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_enum_serialization_producers_require_a_module(self):
        self.write("ya.make", "GENERATE_ENUM_SERIALIZATION(answer.h)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.h", "#pragma once\nenum class Answer { Value = 42 };\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_joined_source_producers_require_a_module(self):
        self.write("ya.make", "JOIN_SRCS(generated.cpp first.cpp second.cpp)\nLIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        for name in ("first", "second", "answer"):
            self.write(name + ".cpp", f"int {name}() {{ return 42; }}\n")
        self.assertNotEqual(self.make().returncode, 0)
