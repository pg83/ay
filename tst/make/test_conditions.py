from tst.make.lib import MakeTest, PROJECT


class ConditionsTest(MakeTest):
    def test_compound_conditions(self):
        expressions = (
            'DEFINED VALUE AND NOT DISABLED',
            'VALUE == "42" OR DISABLED',
            'VALUE > 40 AND VALUE < 43',
            'TEXT STARTS_WITH "prefix"',
            'TEXT ENDS_WITH "suffix"',
            'TEXT MATCHES "middle"',
        )
        self.write("yes.cpp", "int answer() { return 42; }\n")
        self.write("no.cpp", "int other() { return 17; }\n")
        for expression in expressions:
            with self.subTest(expression=expression):
                self.write("ya.make", f'LIBRARY()\nSET(VALUE 42)\nSET(TEXT prefix-middle-suffix)\nSET(DISABLED no)\nIF({expression})\nSRCS(yes.cpp)\nELSE()\nSRCS(no.cpp)\nENDIF()\nEND()\n')
                self.node(self.graph(), "yes.cpp.o")

    def test_source_flags_and_source_directory(self):
        self.write("ya.make", f"LIBRARY()\nSRCDIR({PROJECT}/src)\nADDINCL({PROJECT}/include)\nCFLAGS(-DE2E=42)\nCXXFLAGS(-DE2E_CPP=1)\nSRCS(GLOBAL answer.cpp)\nEND()\n")
        self.write("src/answer.cpp", '#include "value.h"\nint answer() { return VALUE; }\n')
        self.write("include/value.h", "#define VALUE 42\n")
        graph = self.graph()
        self.assertTrue(any(f"$(S)/{PROJECT}/include/value.h" in node["inputs"] for node in graph["graph"]))

    def test_sets_and_else_if(self):
        self.write("ya.make", "LIBRARY()\nSET(SOURCES)\nSET_APPEND(SOURCES one.cpp)\nSET_APPEND(SOURCES two.cpp)\nDEFAULT(CHOICE yes)\nIF(NOT CHOICE)\nSRCS(absent.cpp)\nELSEIF(CHOICE)\nSRCS(${SOURCES})\nENDIF()\nEND()\n")
        self.write("one.cpp", "int one() { return 1; }\n")
        self.write("two.cpp", "int two() { return 2; }\n")
        graph = self.graph()
        self.node(graph, "one.cpp.o")
        self.node(graph, "two.cpp.o")

    def test_include_and_command_line_condition_select_sources(self):
        self.write("ya.make", f"LIBRARY()\nINCLUDE(${{ARCADIA_ROOT}}/{PROJECT}/sources.inc)\nSRCS(${{CHOSEN}})\nEND()\n")
        self.write("sources.inc", "IF(E2E_ALTERNATE)\nSET(CHOSEN second.cpp)\nELSE()\nSET(CHOSEN first.cpp)\nENDIF()\n")
        self.write("first.cpp", "int first() { return 1; }\n")
        self.write("second.cpp", "int second() { return 2; }\n")
        for flag, chosen, excluded in (("no", "first", "second"), ("yes", "second", "first")):
            with self.subTest(flag=flag):
                graph = self.graph(f"-DE2E_ALTERNATE={flag}")
                self.node(graph, chosen + ".cpp.o")
                outputs = [path for node in graph["graph"] for path in node["outputs"]]
                self.assertNotIn(f"$(B)/{PROJECT}/{excluded}.cpp.o", outputs)
