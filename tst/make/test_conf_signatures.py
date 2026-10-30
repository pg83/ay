from tst.make.lib import MakeTest


class ConfSignaturesTest(MakeTest):
    def test_source_macro_validates_arguments_after_configuration_expansion(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_EMPTY_SOURCE() {
    SRC()
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_EMPTY_SOURCE()\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_unterminated_variable_reference_in_a_configuration_macro(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_UNTERMINATED() {
    CFLAGS(-DE2E_VALUE=${UNTERMINATED)
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_UNTERMINATED()\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_recursive_variable_in_a_configuration_macro_is_an_error(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
E2E_GROWING = prefix$E2E_GROWING
macro E2E_RECURSIVE_VALUE() {
    CFLAGS($E2E_GROWING)
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_RECURSIVE_VALUE()\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_membership_conditions_require_whitespace_and_a_list(self):
        self.assert_rejected_conditions((
            '($OS_LINUX in["yes"])', '($OS_LINUX in "yes")',
            '($OS_LINUX in ["yes")', '("yes" in["yes"])',
        ))

    def test_macro_default_argument_with_an_escaped_quote(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + r'''
macro E2E_QUOTED_DEFAULT(VALUE="first\"second") {
    CFLAGS(-DE2E_DEFAULT=$VALUE)
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_QUOTED_DEFAULT()\nEND()\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn('-DE2E_DEFAULT=first"second', args)

    def test_dotted_variable_in_configuration_macro_is_a_syntax_error(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_UNBOUND_DOTTED() {
    CFLAGS(-DE2E_UNBOUND=${E2E.UNBOUND.VALUE})
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_UNBOUND_DOTTED()\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_macro_expansion_validates_builtin_arguments(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_INVALID_SET() {
    SET()
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_INVALID_SET()\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SET", result.stderr)

    def test_recursive_configuration_macro_is_an_error(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_RECURSIVE(WORD) {
    E2E_RECURSIVE($WORD)
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_RECURSIVE(value)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("recurs", result.stderr.lower())

    def test_macro_references_accept_consecutive_internal_dashes(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
E2E--VALUE = chosen
macro E2E_DASHES() {
    CFLAGS(-DE2E_DASH=$E2E--VALUE)
    CFLAGS(${pre=-DE2E_BRACED=:E2E--VALUE})
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_DASHES()\nEND()\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        for flag in ("-DE2E_DASH=chosen", "-DE2E_BRACED=chosen"):
            self.assertIn(flag, args)

    def test_unknown_macro_modifier_is_a_configuration_error(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_UNKNOWN_MODIFIER(WORD) {
    CFLAGS(${e2e_unknown_modifier:WORD})
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_UNKNOWN_MODIFIER(value)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_circular_configuration_imports_are_errors(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '\n@import "${CONF_ROOT}/conf/ay_e2e_first.conf"\n')
        self.write_tree("build/conf/ay_e2e_first.conf", '@import "${CONF_ROOT}/conf/ay_e2e_second.conf"\n')
        self.write_tree("build/conf/ay_e2e_second.conf", '@import "${CONF_ROOT}/conf/ay_e2e_first.conf"\n')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_path_modifiers_on_macro_arguments(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_PATH_MODIFIERS(WORDS...) {
    CFLAGS(${pre=-DNOALL_;noallext:WORDS})
    CFLAGS(${pre=-DBASE_;nopath:WORDS})
    CFLAGS(${pre=-DLAST_;lastext:WORDS})
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_PATH_MODIFIERS(folder/Name.tar.gz Plain)\nEND()\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        for arg in ("-DNOALL_folder/Name", "-DNOALL_Plain", "-DBASE_Name.tar.gz", "-DBASE_Plain", "-DLAST_gz", "-DLAST_"):
            self.assertIn(arg, args)

    def setUp(self):
        super().setUp()
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
macro E2E_BIND(SOURCE, FLAG?"yes":"no", VALUE="default", LIST[], STOP?) {
    SRCS($SOURCE)
    CFLAGS(-DE2E_FLAG=$FLAG -DE2E_VALUE=$VALUE $LIST)
}
macro E2E_TAIL(HEAD, TAIL...) {
    CFLAGS(${pre=-DHEAD=:HEAD} ${pre=-DTAIL=:TAIL})
}
macro E2E_MODIFIERS(WORDS...) {
    CFLAGS(${pre=-DUPPER_;toupper:WORDS})
    CFLAGS(${pre=-DLOWER_;tolower:WORDS})
    CFLAGS(${pre=-DNOEXT_;noext:WORDS})
}
''')
        self.write("answer.cpp", "int answer() { return 42; }\n")

    def assert_rejected_conditions(self, conditions):
        config = self.root / "build/ymake.core.conf"
        base = config.read_text()
        for condition in conditions:
            with self.subTest(condition=condition):
                self.write_tree("build/ymake.core.conf", base + f'''
when {condition} {{
    E2E_INVALID_CONDITION = yes
}}
macro E2E_INVALID_CONDITIONS() {{
    CFLAGS(-DE2E_INVALID_CONDITION=$E2E_INVALID_CONDITION)
}}
''')
                self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_INVALID_CONDITIONS()\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_numeric_configuration_comparisons_are_rejected(self):
        self.assert_rejected_conditions((
            "(12 < 123)", "(12 > 123)", "(12 <= 12)", "(12 >= 12)",
            "(123 >= 12)", "(123 <= 12)", "(12 == 12)", "(12 != 12)",
            '(0 == "")', '(1 == "value")', '(0 < "value")', '("" < 1)',
        ))

    def test_ordered_configuration_comparisons_are_rejected(self):
        self.assert_rejected_conditions((
            '("alpha" < "beta")', '("alpha" > "beta")',
            '("alpha" <= "alpha")', '("alpha" >= "alpha")',
            '("beta" >= "alpha")', '("beta" <= "alpha")',
        ))

    def test_bare_configuration_booleans_are_rejected(self):
        self.assert_rejected_conditions((
            '(yes == no)', '(yes != no)',
        ))

    def test_escaped_quotes_in_configuration_conditions(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + r'''
E2E_ESCAPED = no
when ($OS_LINUX == "un\"used" || $OS_LINUX == "un\\used") {
    E2E_ESCAPED = yes
}
macro E2E_ESCAPE() {
    CFLAGS(-DE2E_ESCAPED=$E2E_ESCAPED)
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_ESCAPE()\nEND()\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DE2E_ESCAPED=no", args)

    def test_malformed_configuration_conditions_are_rejected(self):
        self.assert_rejected_conditions((
            '$OS_LINUX == "yes"', '($OS_LINUX == "yes"',
            '($OS_LINUX ==)', '(("a" == "a")', '(== "a")',
            '("a" == "a" == "a")', '("a" == "a" <= "a")',
            '("a" == "a" < "a")', '("a" == "a" > "a")',
            '("a" == "a" >= "a")', '("a" == "a" != "a")',
            '("a" == "a" garbage)', '(BARE_VARIABLE == "yes")',
            '("unterminated)', '($ in ["yes"])',
            '("yes" in [unquoted])', '("yes" in ["unterminated])',
            '("yes" in ["yes" "no"])', '("yes" in [])',
            '(12)', '(0)',
        ))

    def test_default_and_exclusion_assignments(self):
        config = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", config.read_text() + '''
E2E_DEFAULT := first
E2E_DEFAULT := second
E2E_WORDS = omitkeepomit
E2E_WORDS -= omit
E2E_EMPTY = empty
E2E_EMPTY -=
macro E2E_ASSIGNMENTS() {
    CFLAGS(-DDEFAULT=$E2E_DEFAULT -DWORDS=$E2E_WORDS -DEMPTY=$E2E_EMPTY)
}
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_ASSIGNMENTS()\nEND()\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        for arg in ("-DDEFAULT=first", "-DWORDS=keep", "-DEMPTY=empty"):
            self.assertIn(arg, args)

    def test_malformed_macro_signatures_are_configuration_errors(self):
        config = self.root / "build/ymake.core.conf"
        base = config.read_text()
        parameters = ('VALUE=unquoted', 'VALUE="unterminated', 'VALUE="ok" trailing',
                      'FLAG?"yes":"no" trailing', 'FLAG?"yes":unquoted', 'NAME!',
                      'NAME{unterminated', 'NAME{type}garbage')
        for params in parameters:
            with self.subTest(parameters=params):
                self.write_tree("build/ymake.core.conf", base + f"\nmacro E2E_BROKEN({params}) {{\n    CFLAGS(-DBROKEN)\n}}\n")
                self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_malformed_configuration_blocks_and_imports(self):
        config = self.root / "build/ymake.core.conf"
        base = config.read_text()
        suffixes = ('}', 'when ($OS_LINUX == "yes") {', '@import "missing.conf"',
                    '@import "${CONF_ROOT}/ay_e2e_missing.conf"',
                    'macro E2E_BROKEN {\n    CFLAGS(-DBROKEN)\n}',
                    'macro E2E_BROKEN(NAME {\n    CFLAGS(-DBROKEN)\n}')
        for suffix in suffixes:
            with self.subTest(configuration=suffix):
                self.write_tree("build/ymake.core.conf", base + "\n" + suffix + "\n")
                self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
                self.assertNotEqual(self.make().returncode, 0)

    def test_scalar_flags_defaults_and_repeated_arrays(self):
        calls = (
            ("E2E_BIND(answer.cpp)", ("-DE2E_FLAG=no", "-DE2E_VALUE=default")),
            ("E2E_BIND(answer.cpp FLAG VALUE chosen LIST -DFIRST LIST -DSECOND)",
             ("-DE2E_FLAG=yes", "-DE2E_VALUE=chosen", "-DFIRST", "-DSECOND")),
        )
        for call, expected in calls:
            with self.subTest(call=call):
                self.write("ya.make", f"LIBRARY()\n{call}\nEND()\n")
                node = self.node(self.graph(), "answer.cpp.o")
                args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
                for arg in expected:
                    self.assertEqual(args.count(arg), 1)

    def test_invalid_macro_arguments_are_configuration_errors(self):
        calls = (
            "E2E_BIND()", "E2E_BIND(answer.cpp extra)",
            "E2E_BIND(answer.cpp FLAG FLAG)", "E2E_BIND(answer.cpp VALUE)",
            "E2E_BIND(answer.cpp VALUE FLAG)", "E2E_BIND(answer.cpp VALUE first VALUE second)",
            "E2E_BIND(answer.cpp LIST -DFIRST FLAG extra)",
        )
        for call in calls:
            with self.subTest(call=call):
                self.write("ya.make", f"LIBRARY()\n{call}\nEND()\n")
                result = self.make()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("E2E_BIND", result.stderr)

    def test_variadic_arguments_and_case_and_extension_modifiers(self):
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nE2E_TAIL(first second third)\nE2E_MODIFIERS(Mixed.tar.gz Plain)\nEND()\n")
        node = self.node(self.graph(), "answer.cpp.o")
        args = [arg for command in node["cmds"] for arg in command["cmd_args"]]
        for arg in ("-DHEAD=first", "-DTAIL=second", "-DTAIL=third",
                    "-DUPPER_MIXED.TAR.GZ", "-DUPPER_PLAIN", "-dlower_mixed.tar.gz", "-dlower_plain",
                    "-DNOEXT_Mixed.tar", "-DNOEXT_Plain"):
            self.assertIn(arg, args)
