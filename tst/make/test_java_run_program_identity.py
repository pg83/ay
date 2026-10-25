import hashlib
import unittest

from tst import lib


def tree(before="", arguments="Main", after="", out_dir="generated"):
    return lib.java_tree(**{
        "build/platform/java/scriptgen/ya.make": lib.resource_lib("SCRIPTGEN", 9),
        "grove/tool/ya.make": "JAVA_PROGRAM(tool)\nNO_LINT()\nJAR_MAIN_CLASS(Main)\nJAVA_SRCS(Main.java)\nEND()\n",
        "grove/tool/Main.java": "public class Main {}\n",
        "grove/gen/input.txt": "seed\n",
        "grove/gen/ya.make": (
            "JAVA_LIBRARY_NON_SPLIT(bundle)\nNO_LINT()\n" + before +
            f"RUN_JAVA_PROGRAM({arguments} IN input.txt OUT_DIR {out_dir} CLASSPATH grove/tool)\n" +
            after + "END()\n"
        ),
    })


class JavaRunProgramIdentityTest(unittest.TestCase):
    def assert_identity(self, graph, arguments, out_dir="generated"):
        preimage = f"{arguments} IN input.txt OUT_DIR {out_dir} CLASSPATH grove/tool"
        salt = hashlib.md5(preimage.encode()).hexdigest()
        output = f"$(B)/grove/gen/generated.{salt}.gentar"
        generated = lib.node_by_output(graph, output)
        self.assertEqual(generated["kv"]["p"], "RJ")
        self.assertIn("$(S)/grove/gen/input.txt", generated["inputs"])
        tool = lib.node_by_output(graph, "$(B)/grove/tool/tool.run.cp.jar")
        self.assertIn(tool["outputs"][0], generated["inputs"])
        self.assertIn(tool["uid"], generated["deps"])
        self.assertEqual(generated["cmds"][-1]["cmd_args"][-1], output)
        consumer = lib.node_by_output(graph, "$(B)/grove/gen/bundle.jar")
        self.assertIn(output, consumer["inputs"])
        self.assertIn(generated["uid"], consumer["deps"])
        self.assertIn(consumer["uid"], graph["result"])
        return generated

    def test_bare_braced_and_literal_arguments_have_one_native_identity(self):
        for before, arguments, expected in (
            ("", "Main", "Main"),
            ("DEFAULT(OPTS)\n", "Main $OPTS", "Main"),
            ("SET(OPTS)\n", "Main ${OPTS}", "Main"),
            ("SET(OPTS)\nDEFAULT(OPTS --token pine)\n", "Main $OPTS", "Main"),
            ("SET(OPTS --token pine)\n", "Main $OPTS", "Main --token pine"),
            ("SET(OPTS --token pine)\n", "Main ${OPTS}", "Main --token pine"),
            ("SET(OPTS  --token   pine)\n", "Main    $OPTS", "Main --token pine"),
            ("", "Main $MISSING", "Main $MISSING"),
            ("", "Main ${MISSING}", "Main"),
        ):
            with self.subTest(before=before, arguments=arguments):
                graph = lib.make(tree(before, arguments), "grove/gen")
                self.assert_identity(graph, expected)

    def test_digest_freezes_call_site_value_and_expands_long_bare_chain(self):
        before = "SET(OPTS --token old)\n"
        graph = lib.make(tree(before, "Main $OPTS", "SET(OPTS --token new)\n"), "grove/gen")
        self.assert_identity(graph, "Main --token old")
        before = "".join(f"SET(A{i} $A{i + 1})\n" for i in range(11)) + "SET(A11 --token pine)\n"
        graph = lib.make(tree(before, "Main $A0"), "grove/gen")
        self.assert_identity(graph, "Main --token pine")

    def test_cli_default_and_configuration_scalars_bind_before_hashing(self):
        for ref in ("$OPTS", "${OPTS}"):
            with self.subTest(ref=ref):
                graph = lib.make(tree("DEFAULT(OPTS)\n", "Main " + ref), "grove/gen", "-DOPTS=--token pine")
                self.assert_identity(graph, "Main --token pine")
        graph = lib.make(tree("DEFAULT(OPTS --token pine)\n", "Main $OPTS"), "grove/gen", "-DOPTS=")
        self.assert_identity(graph, "Main")
        graph = lib.make(tree(arguments="Main --revision $JDK_LATEST_VERSION"), "grove/gen")
        self.assert_identity(graph, "Main --revision 26")

    def test_lexical_quotes_and_symbolic_path_names_survive_hash_expansion(self):
        graph = lib.make(tree("SET(OPTS pine)\n", 'Main --token="$OPTS" "a b"'), "grove/gen")
        self.assert_identity(graph, 'Main --token="pine" a b')
        graph = lib.make(tree("SET(OPTS --token pine)\n", "Main $OPTS --root $CURDIR"), "grove/gen")
        self.assert_identity(graph, "Main --token pine --root $CURDIR")
        graph = lib.make(tree("SET(DEST ${BINDIR}/generated)\nSET(OPTS --token pine)\n",
                              "Main $OPTS", out_dir="${DEST}"), "grove/gen")
        self.assert_identity(graph, "Main --token pine", out_dir="${BINDIR}/generated")

    def test_recursive_configuration_variables_are_rejected(self):
        result = lib.make_process(tree("SET(LOOP $OTHER)\nSET(OTHER $LOOP)\n", "Main $LOOP"), "grove/gen")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("recursive variable expansion", result.stderr)


if __name__ == "__main__":
    unittest.main()
