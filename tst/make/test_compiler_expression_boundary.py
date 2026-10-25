"""Native command arguments follow source binding, without replaying the source."""
import unittest

from tst import lib


def library(body):
    return f"LIBRARY()\n{lib.BARE_MODULE}{body}END()\n"


def fixture(body):
    return {
        "willow/ya.make": library(body + "SRCS(branch.cpp leaf.c)\n"),
        "willow/branch.cpp": "int branch;\n",
        "willow/leaf.c": "int leaf;\n",
    }


def command(graph, directory="willow", source="branch.cpp"):
    for suffix in (".o", ".pic.o"):
        for node in graph["graph"]:
            if f"$(B)/{directory}/{source}{suffix}" in node["outputs"]:
                return node["cmds"][0]["cmd_args"]
    raise AssertionError(f"missing native object for {directory}/{source}")


def flags(graph, directory="willow", source="branch.cpp"):
    args = command(graph, directory, source)
    first = args.index("-D__LONG_LONG_SUPPORTED") + 1
    return args[first:args.index("-UNDEBUG", first)]


class CompilerExpressionBoundaryTest(unittest.TestCase):
    def test_bare_short_roots_are_ordinary_undefined_command_variables(self):
        graph = lib.make(fixture(
            "CFLAGS(-DSHORT_SOURCE=$S/path -DSHORT_BUILD=$B/path $S $B $ABSENT "
            "-DEMPTY=$ABSENT $ -DDASH=$-a)\n"
        ), "willow")
        self.assertEqual(flags(graph), ["-DSHORT_SOURCE=/path", "-DSHORT_BUILD=/path",
                                       "-DEMPTY=", "$", "-DDASH=$-a"])

    def test_late_words_splice_standalone_and_concatenate_inside_arguments(self):
        graph = lib.make(fixture(
            "CFLAGS(-DEARLY=${WORDS} -DLATE=$WORDS $WORDS -DPAIR=$WORDS$WORDS)\n"
            "CXXFLAGS(-DCXX=$WORDS)\nCONLYFLAGS(-DC=$WORDS)\n"
            "SET(WORDS one two)\n"
        ), "willow")
        expected = ["-DEARLY=", "-DLATE=onetwo", "one", "two", "-DPAIR=onetwoonetwo"]
        self.assertEqual(flags(graph), expected)
        self.assertEqual(flags(graph, source="leaf.c"), expected)
        self.assertIn("-DCXX=onetwo", command(graph))
        self.assertIn("-DC=onetwo", command(graph, source="leaf.c"))

    def test_source_snapshots_and_late_aliases_have_distinct_binding_times(self):
        graph = lib.make(fixture(
            "SET(EARLY captured)\nSET(ALIAS \\$FUTURE)\n"
            "CFLAGS(-DEARLY=${EARLY} -DALIAS=${ALIAS} -DBARE=$FUTURE)\n"
            "SET(EARLY changed)\nSET(FUTURE arrived)\n"
        ), "willow")
        self.assertEqual(flags(graph), ["-DEARLY=captured", "-DALIAS=arrived", "-DBARE=arrived"])

    def test_formal_flags_shadowing_ends_before_final_command_expansion(self):
        files = fixture('CFLAGS($Flags)\nIF(USER_CFLAGS == "$Flags")\n'
                        'CXXFLAGS(-DSOURCE_SCOPE)\nENDIF()\n')
        files["build/ymake_conf.py"] = lib.ymake_conf(extra="Flags=outer\n")
        graph = lib.make(files, "willow")
        self.assertEqual(flags(graph), ["outer"])
        self.assertIn("-DSOURCE_SCOPE", command(graph))

    def test_shared_lexical_include_resolves_late_values_in_each_owner(self):
        files = {"policy/flags.inc": "CFLAGS(-DLOCAL=$LATE GLOBAL -DEXPORTED=$LATE)\n"}
        for directory, value in (("willow", "blue"), ("alder", "green")):
            files[f"{directory}/ya.make"] = library(
                "INCLUDE(${ARCADIA_ROOT}/policy/flags.inc)\n"
                f"SET(LATE {value})\nSRCS(branch.cpp)\n"
            )
            files[f"{directory}/branch.cpp"] = "int branch;\n"
        files["spruce/ya.make"] = library("PEERDIR(willow alder)\nSET(LATE consumer)\nSRCS(branch.cpp)\n")
        files["spruce/branch.cpp"] = "int consumer;\n"
        for roots in (("spruce",), ("willow", "alder", "spruce"), ("alder", "willow", "spruce")):
            with self.subTest(roots=roots):
                graph = lib.make(files, *roots)
                if len(roots) > 1:
                    self.assertEqual(flags(graph, "willow"), ["-DLOCAL=blue", "-DEXPORTED=blue"])
                    self.assertEqual(flags(graph, "alder"), ["-DLOCAL=green", "-DEXPORTED=green"])
                self.assertEqual(flags(graph, "spruce"), ["-DEXPORTED=blue", "-DEXPORTED=green"])

    def test_escaped_refs_roots_and_quoted_whitespace_survive_final_lowering(self):
        graph = lib.make(fixture(
            "SET(VALUE before)\n" +
            r'CFLAGS(-DBRACED=\\\${VALUE} -DBARE=\\\$VALUE -DROOT=\\\${ARCADIA_ROOT} -DSHORT=\\\$S -DQ="$LATE" -DEMPTY="$ABSENT")' +
            "\nSET(VALUE after)\nSET(LATE one two)\n"
        ), "willow")
        self.assertEqual(flags(graph), ["-DBRACED=${VALUE}", "-DBARE=$VALUE",
                                       "-DROOT=${ARCADIA_ROOT}", "-DSHORT=$S",
                                       "-DQ=onetwo", "-DEMPTY="])

    def test_long_roots_and_generated_input_tool_dependencies_remain_real(self):
        files = fixture(
            "CFLAGS(-DSOURCE=${ARCADIA_ROOT}/asset -DBUILD=${ARCADIA_BUILD_ROOT}/asset "
            "-DLOCAL=${CURDIR}/asset -DOUTPUT=${BINDIR}/asset -DLATE=$LATE)\n"
            "SET(LATE bound)\nRUN_PROGRAM(tools/write OUT generated.cpp)\n"
        )
        lib.tool_program(files, "tools/write", "renamed-writer")
        graph = lib.make(files, "willow")
        expected = ["-DSOURCE=$(S)/asset", "-DBUILD=$(B)/asset", "-DLOCAL=$(S)/willow/asset",
                    "-DOUTPUT=$(B)/willow/asset", "-DLATE=bound"]
        self.assertEqual(flags(graph), expected)
        self.assertEqual(flags(graph, source="generated.cpp"), expected)
        generated = lib.node_by_output(graph, "$(B)/willow/generated.cpp")
        tool = lib.node_by_output(graph, "$(B)/tools/write/renamed-writer")
        compiled = lib.node_by_output(graph, "$(B)/willow/generated.cpp.o")
        self.assertIn(tool["uid"], generated["deps"])
        self.assertIn(tool["outputs"][0], generated["inputs"])
        self.assertIn(generated["uid"], compiled["deps"])
        self.assertIn(generated["outputs"][0], compiled["inputs"])
        self.assertIn("$(S)/willow/branch.cpp", lib.node_by_output(graph, "$(B)/willow/branch.cpp.o")["inputs"])

    def test_resource_roots_bind_after_source_conditions_and_peer_resolution(self):
        for own in (False, True):
            with self.subTest(own=own):
                body = ("CFLAGS(-DRESOURCE=${MINT_RESOURCE_GLOBAL}/include)\n" +
                        r'CFLAGS(-DLITERAL=\\\${MINT_RESOURCE_GLOBAL}/include)' + "\n")
                declaration = "DECLARE_EXTERNAL_RESOURCE(MINT sbr:731)\n"
                files = fixture(body + (declaration if own else "PEERDIR(resource)\n"))
                files["resource/ya.make"] = "RESOURCES_LIBRARY()\n" + declaration + "END()\n"
                graph = lib.make(files, "willow")
                self.assertEqual(flags(graph), ["-DRESOURCE=$(B)/resources/MINT/include",
                                               "-DLITERAL=${MINT_RESOURCE_GLOBAL}/include"])
                resource = lib.node_by_output(graph, "$(B)/resources/MINT")
                self.assertIn("sbr:731", resource["cmds"][0]["cmd_args"])

    def test_effective_malformed_expressions_are_command_errors(self):
        for expression in ("-Dn=${X ${X}}", "-DUNFINISHED=${", "${"):
            with self.subTest(expression=expression):
                result = lib.make_process(fixture(f"SET(X foo)\nCFLAGS({expression})\n"), "willow")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("could not parse compiler command", result.stderr)

    def test_final_quoted_literals_and_unicode_space(self):
        files = fixture("CFLAGS($LITERALS -DSPACE=left\u2003right -DTAIL=$LATE-dash)\nSET(LATE final)\n")
        files["build/ymake_conf.py"] = lib.ymake_conf(
            extra=r'LITERALS=-DONE=${"literal"} -DESC=${"a\q"} -DQUOTE=${"a\\b\"c"}' + "\n"
        )
        self.assertEqual(flags(lib.make(files, "willow")),
                         ["-DONE=literal", r"-DESC=a\q", '-DQUOTE=a\\b"c',
                          "-DSPACE=left", "right", "-DTAIL=final-dash"])

    def test_final_command_rejects_incomplete_escapes_quotes_strings_and_cycles(self):
        bodies = [
            r'CFLAGS(tail\ GLOBAL)' + '\n',
            'CFLAGS($LATE)\nSET(LATE ' + '\\' + chr(34) + 'unfinished)\n',
            "CFLAGS($LATE)\nSET(LATE $LATE)\n",
        ]
        for body in bodies:
            with self.subTest(body=body):
                result = lib.make_process(fixture(body), "willow")
                self.assertNotEqual(result.returncode, 0)
                self.assertTrue("could not parse compiler command" in result.stderr or
                                "self-contradictory variable definition" in result.stderr, result.stderr)
        files = fixture("CFLAGS($BROKEN)\n")
        files["build/ymake_conf.py"] = lib.ymake_conf(extra='BROKEN=${"unterminated}\n')
        result = lib.make_process(files, "willow")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unclosed string", result.stderr)

    def test_resource_library_exports_bind_in_the_resource_owner(self):
        files = fixture("PEERDIR(resource)\nSET(LATE consumer)\n")
        files["resource/ya.make"] = (
            "RESOURCES_LIBRARY()\nCFLAGS(GLOBAL -DRESOURCE=$MINT_RESOURCE_GLOBAL GLOBAL -DOWNER=$LATE)\n"
            "SET(LATE resource_owner)\nDECLARE_EXTERNAL_RESOURCE(MINT sbr:731)\nEND()\n"
        )
        self.assertEqual(flags(lib.make(files, "willow")),
                         ["-DRESOURCE=$(B)/resources/MINT", "-DOWNER=resource_owner"])

    def test_dynamic_library_vcs_compile_uses_final_bound_flags(self):
        files = fixture("")
        files["dynamic/ya.make"] = (
            "DYNAMIC_LIBRARY(twig)\n" + lib.BARE_MODULE +
            "CFLAGS(-DDYNAMIC=$LATE GLOBAL -DEXPORT=$LATE)\n"
            "CXXFLAGS(GLOBAL -DCXX=$LATE)\nCONLYFLAGS(GLOBAL -DC=$LATE)\n"
            "SET(LATE bound)\nEXPORTS_SCRIPT(twig.exports)\nDYNAMIC_LIBRARY_FROM(willow)\nEND()\n"
        )
        files["dynamic/twig.exports"] = "{};\n"
        files["build/platform/local_so/ya.make"] = library("")
        lib.tool_program(files, "tools/fix_elf", "fix_elf")
        node = lib.node_by_output(lib.make(files, "dynamic"), "$(B)/dynamic/libtwig.so")
        vcs = next(c["cmd_args"] for c in node["cmds"]
                   if "$(B)/dynamic/__vcs_version__.c" in c["cmd_args"] and "-c" in c["cmd_args"])
        self.assertIn("-DDYNAMIC=bound", vcs)

    def test_configured_runtime_tokens_and_empty_quoted_arguments(self):
        files = fixture("CFLAGS($RUNTIME $QUOTED -DSHORT=$S)\nSET(S ordinary_value)\n")
        files["build/ymake_conf.py"] = lib.ymake_conf(
            extra='RUNTIME=-DRUNTIME=$(SHELL_ROOT)/path\nQUOTED=""\n'
        )
        self.assertEqual(flags(lib.make(files, "willow")),
                         ["-DRUNTIME=$(SHELL_ROOT)/path", "", "-DSHORT=ordinary_value"])

    def test_raw_runtime_tokens_unescape_without_expanding_their_contents(self):
        files = fixture("CFLAGS($RUNTIME)\n")
        files["build/ymake_conf.py"] = lib.ymake_conf(extra=r'RUNTIME=-DRUNTIME=$(path\ segment/$S)' + "\n")
        self.assertEqual(flags(lib.make(files, "willow")), ["-DRUNTIME=$(path segment/$S)"])

    def test_quoted_bare_dollars_and_unprotected_command_separators_are_errors(self):
        for call in ('-DQUOTED="$-a"', 'first&&second', 'first&second'):
            with self.subTest(call=call):
                result = lib.make_process(fixture(f"CFLAGS({call})\n"), "willow")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("could not parse compiler command", result.stderr)

    def test_unused_private_expressions_are_not_compiled(self):
        graph = lib.make({"unused/ya.make": library("CFLAGS(-DUNFINISHED=${)\n")}, "unused")
        self.assertFalse(any(n["kv"].get("p") == "CC" for n in graph["graph"]))
        for macro, source in (("CXXFLAGS", "leaf.c"), ("CONLYFLAGS", "branch.cpp")):
            with self.subTest(macro=macro):
                files = fixture(f"{macro}(-DUNFINISHED=${{)\n")
                files["willow/ya.make"] = files["willow/ya.make"].replace(
                    "SRCS(branch.cpp leaf.c)", f"SRCS({source})")
                self.assertEqual(flags(lib.make(files, "willow"), source=source), [])

    def test_protected_separators_are_literal_arguments(self):
        graph = lib.make(fixture("CFLAGS(&& ;)\n"), "willow")
        self.assertEqual(flags(graph), ["&&", ";"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
