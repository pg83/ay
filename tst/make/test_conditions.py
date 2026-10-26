from tst.make.lib import MakeTest, PROJECT


class ConditionsTest(MakeTest):
    def test_empty_conditions_choose_else(self):
        self.write("ya.make", "LIBRARY()\nIF()\nSRCS(first.cpp)\nELSEIF()\nSRCS(second.cpp)\nELSE()\nSRCS(answer.cpp)\nENDIF()\nEND()\n")
        for name in ("first", "second", "answer"):
            self.write(name + ".cpp", f"int {name}() {{ return 42; }}\n")
        graph = self.graph()
        self.node(graph, "answer.cpp.o")
        outputs = [path for node in graph["graph"] for path in node["outputs"]]
        for name in ("first", "second"):
            self.assertNotIn(f"$(B)/{PROJECT}/{name}.cpp.o", outputs)

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

    def test_condition_operand_evaluation(self):
        # An unquoted operand names a variable, a $-operand is expanded, quotes
        # inside an atom are literal; DEFINED/ISNUM yield yes/no strings.
        cases = (
            ("", "yes", False),
            ("", 'abc-def == ""', True),
            ("SET(X yes)", '"X"', True),
            ("SET(X no)", "X==yes", False),
            ('SET(X x"y")', 'X == x"y"', True),
            ("SET(X no)", "$X", True),
            ("SET(X no)", "NOT ${X}", True),
            ("SET(X no)", '"${X}"', False),
            ("SET(X 1)", "DEFINED X == yes", True),
            ("SET(X 5)", "ISNUM X", True),
            ("SET(X 5x)", "ISNUM X", False),
            ("", "ISNUM 5", True),
            ("SET(V 1.2)", "V VERSION_LT 1.2.0", True),
            ("", "ANDROID_API", False),
        )
        self.write("yes.cpp", "int answer() { return 42; }\n")
        self.write("no.cpp", "int other() { return 17; }\n")
        for setup, expression, taken in cases:
            with self.subTest(setup=setup, expression=expression):
                self.write("ya.make", f"LIBRARY()\n{setup}\nIF({expression})\nSRCS(yes.cpp)\nELSE()\nSRCS(no.cpp)\nENDIF()\nEND()\n")
                graph = self.graph()
                chosen, other = ("yes", "no") if taken else ("no", "yes")
                self.node(graph, chosen + ".cpp.o")
                outputs = [path for node in graph["graph"] for path in node["outputs"]]
                self.assertNotIn(f"$(B)/{PROJECT}/{other}.cpp.o", outputs)

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

    def test_conf_condition_in_string_list(self):
        # sdc.conf sets JETSON=yes when ($SDC_PLATFORM in [...]) lists the platform.
        self.write("ya.make", 'LIBRARY()\nIF(JETSON == "yes")\nSRCS(yes.cpp)\nELSE()\nSRCS(no.cpp)\nENDIF()\nEND()\n')
        self.write("yes.cpp", "int answer() { return 42; }\n")
        self.write("no.cpp", "int other() { return 17; }\n")
        for platform, chosen, excluded in (("car-jp6", "yes", "no"), ("x86_64", "no", "yes")):
            with self.subTest(platform=platform):
                graph = self.graph(f"-DSDC_PLATFORM={platform}")
                self.node(graph, chosen + ".cpp.o")
                outputs = [path for node in graph["graph"] for path in node["outputs"]]
                self.assertNotIn(f"$(B)/{PROJECT}/{excluded}.cpp.o", outputs)
