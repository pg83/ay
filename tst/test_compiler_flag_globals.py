import unittest

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def library(body):
    return "LIBRARY()\n" + BARE + body + "END()\n"


def export(letter):
    return (
        "CFLAGS(GLOBAL -DSAME GLOBAL -D" + letter + ")\n"
        "CXXFLAGS(GLOBAL -DCXX_SAME GLOBAL -DCXX_" + letter + ")\n"
        "CONLYFLAGS(GLOBAL -DC_SAME GLOBAL -DC_" + letter + ")\n"
    )


def fixture(body):
    return {
        "mod/ya.make": library(body + "SRCS(main.cpp plain.c)\n"),
        "mod/main.cpp": "", "mod/plain.c": "",
        "a/ya.make": library(export("A")),
        "b/ya.make": library(export("B")),
        "same_a/ya.make": library(export("A")),
        "middle/ya.make": library("PEERDIR(a b)\n"),
        "self/ya.make": library("PEERDIR(a)\n" + export("A")),
    }


class CompilerFlagGlobalsTest(unittest.TestCase):
    def check_flags(self, graph, common, cxx, conly):
        for source, tail in (("main.cpp", cxx), ("plain.c", conly)):
            with self.subTest(source=source):
                node = lib.node_by_output(graph, "$(B)/mod/" + source + ".o")
                args = node["cmds"][0]["cmd_args"]
                keep = {"-DSAME", "-DA", "-DB", "-DOWN", "-DCXX_SAME", "-DCXX_A",
                        "-DCXX_B", "-DCXX_OWN", "-DC_SAME", "-DC_A", "-DC_B", "-DC_OWN"}
                self.assertEqual([a for a in args if a in keep], common + tail)

    def test_exports_are_unique_whole_values_with_diamond_peers(self):
        for peers in ("a b same_a middle", "a same_a middle", "self b"):
            with self.subTest(peers=peers):
                graph = lib.make(fixture("PEERDIR(" + peers + ")\n"), "mod")
                self.check_flags(graph, ["-DSAME", "-DA", "-DSAME", "-DB"],
                                 ["-DCXX_SAME", "-DCXX_A", "-DCXX_SAME", "-DCXX_B"],
                                 ["-DC_SAME", "-DC_A", "-DC_SAME", "-DC_B"])

    def test_own_exports_append_after_inherited_values(self):
        for letter in ("A", "OWN"):
            with self.subTest(letter=letter):
                graph = lib.make(fixture("PEERDIR(a)\n" + export(letter)), "mod")
                self.check_flags(graph, ["-DSAME", "-DA", "-DSAME", "-D" + letter],
                                 ["-DCXX_SAME", "-DCXX_A", "-DCXX_SAME", "-DCXX_" + letter],
                                 ["-DC_SAME", "-DC_A", "-DC_SAME", "-DC_" + letter])

    def test_macro_expansion_splits_spaces_before_global_projection(self):
        files = fixture("PEERDIR(a b c d)\n")
        files["a/ya.make"] = library('CFLAGS(GLOBAL "-DQ=a b" GLOBAL -DSAME)\n')
        files["b/ya.make"] = library('CFLAGS(GLOBAL -DQ=a GLOBAL b GLOBAL -DSAME)\n')
        files["c/ya.make"] = library('CFLAGS(GLOBAL "-DQ=a  b" GLOBAL -DSAME)\n')
        files["d/ya.make"] = files["b/ya.make"]
        graph = lib.make(files, "mod")
        for source in ("main.cpp", "plain.c"):
            args = lib.node_by_output(graph, "$(B)/mod/" + source + ".o")["cmds"][0]["cmd_args"]
            self.assertEqual([a for a in args if a.startswith("-DQ=") or a in ("-DSAME", "b")],
                             ["-DQ=a", "-DSAME", "-DQ=a", "b", "-DSAME"])

    def test_local_flags_keep_embedded_quotes_after_native_forwarding(self):
        # CmdParser::UnescapeQuoted retains one slash for an unknown escape.
        for macro in ("CFLAGS", "CXXFLAGS", "CONLYFLAGS"):
            with self.subTest(macro=macro):
                files = fixture(macro + '("-DOUTER=a b" -DINNER="c d" -DQUOTE\'e f\' -DRAW=a\\q -DESC="a\\q")\n')
                graph = lib.make(files, "mod")
                source = "plain.c" if macro == "CONLYFLAGS" else "main.cpp"
                args = lib.node_by_output(graph, "$(B)/mod/" + source + ".o")["cmds"][0]["cmd_args"]
                self.assertEqual([a for a in args if a.startswith(("-DOUTER", "-DINNER", "-DQUOTE", "-DRAW", "-DESC")) or a == "b"],
                                 ["-DOUTER=a", "b", "-DINNER=c d", "-DQUOTEe f", "-DRAW=aq", "-DESC=a\\q"])

    def test_export_identity_keeps_quotes_that_command_expansion_removes(self):
        files = fixture("PEERDIR(a b)\n")
        files["a/ya.make"] = library('CFLAGS(GLOBAL -DQ="a" GLOBAL -DSAME)\n')
        files["b/ya.make"] = library('CFLAGS(GLOBAL -DQ=a GLOBAL -DSAME)\n')
        graph = lib.make(files, "mod")
        args = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")["cmds"][0]["cmd_args"]
        self.assertEqual([a for a in args if a.startswith("-DQ=") or a == "-DSAME"],
                         ["-DQ=a", "-DSAME", "-DQ=a", "-DSAME"])

    def test_empty_forwarded_arguments_do_not_consume_global(self):
        files = fixture("PEERDIR(a)\n")
        files["a/ya.make"] = library('CFLAGS(GLOBAL "" -DSAME GLOBAL "  " -DA)\n')
        graph = lib.make(files, "mod")
        self.check_flags(graph, ["-DSAME", "-DA"], [], [])

    def test_resource_library_exports_use_the_same_value_rule(self):
        files = fixture("PEERDIR(resources same_a b middle)\n")
        files["resources/ya.make"] = "RESOURCES_LIBRARY()\n" + export("A") + "END()\n"
        graph = lib.make(files, "mod")
        self.check_flags(graph, ["-DSAME", "-DA", "-DSAME", "-DB"],
                         ["-DCXX_SAME", "-DCXX_A", "-DCXX_SAME", "-DCXX_B"],
                         ["-DC_SAME", "-DC_A", "-DC_SAME", "-DC_B"])

    def test_system_stl_does_not_add_the_libcxx_proxy_header_flag(self):
        graph = lib.make(fixture("PEERDIR(a b)\n"), "mod", "-DUSE_STL_SYSTEM=yes")
        args = lib.node_by_output(graph, "$(B)/mod/main.cpp.o")["cmds"][0]["cmd_args"]
        self.assertNotIn("-nostdinc++", args)
        self.check_flags(graph, ["-DSAME", "-DA", "-DSAME", "-DB"],
                         ["-DCXX_SAME", "-DCXX_A", "-DCXX_SAME", "-DCXX_B"],
                         ["-DC_SAME", "-DC_A", "-DC_SAME", "-DC_B"])

    def test_repeated_tokens_inside_one_export_value_are_preserved(self):
        graph = lib.make(fixture(
            "CFLAGS(GLOBAL -DSAME GLOBAL -DSAME)\nCFLAGS(GLOBAL -DA)\n"
            "CXXFLAGS(GLOBAL -DCXX_SAME GLOBAL -DCXX_SAME)\nCXXFLAGS(GLOBAL -DCXX_A)\n"
            "CONLYFLAGS(GLOBAL -DC_SAME GLOBAL -DC_SAME)\nCONLYFLAGS(GLOBAL -DC_A)\n"
        ), "mod")
        self.check_flags(graph, ["-DSAME", "-DSAME", "-DA"],
                         ["-DCXX_SAME", "-DCXX_SAME", "-DCXX_A"],
                         ["-DC_SAME", "-DC_SAME", "-DC_A"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
