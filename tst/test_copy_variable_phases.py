import unittest

import lib


CASES = {
    "bare": "COPY_FILE($UNBOUND literal.h)\nCOPY_FILE($UNBOUND.h bare.h)\nCOPY_FILE(prefix$UNBOUND.h interior.h)\nCOPY_FILE_WITH_CONTEXT($UNBOUND.h context.h)\n",
    "braced": "COPY_FILE(${UNBOUND}raw.h braced.h)\nCOPY_FILE(prefix${UNBOUND}raw.h interior.h)\n",
    "bound": "SET(FILE raw.h)\nSET(STEM raw)\nSET(INDIRECT $FILE)\nCOPY_FILE($FILE bare.h)\nCOPY_FILE(${FILE} braced.h)\nCOPY_FILE($STEM.h embedded.h)\nCOPY_FILE($INDIRECT chain.h)\n",
    "destinations": "SET(DEST chosen.h)\nCOPY_FILE(raw.h $UNBOUND.h)\nCOPY_FILE(raw.h ${UNBOUND}braced.h)\nCOPY_FILE(raw.h $DEST)\n",
}


def fixture(cases=CASES):
    files = {}
    roots = []
    for name, body in cases.items():
        root = "sprig/" + name
        roots.append(root)
        files[root + "/ya.make"] = "UNION()\n" + body + "END()\n"
        for path in ("$UNBOUND", "$UNBOUND.h", "prefix$UNBOUND.h", "raw.h", "prefixraw.h"):
            files[root + "/" + path] = '#include "deep.h"\n'
        files[root + "/deep.h"] = "#pragma once\n"
    files["build/scripts/fs_tools.py"] = "import process_command_files\n"
    files["build/scripts/process_command_files.py"] = "# graph-only fixture\n"
    return files, roots


class CopyVariablePhasesTest(unittest.TestCase):
    def graph(self, cases, reverse=False):
        files, roots = fixture({name: CASES[name] for name in cases})
        graph = lib.make(files, *(list(reversed(roots)) if reverse else roots), "--sandboxing")
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))
        return graph

    def copied(self, graph, case, output, source):
        prefix = "$(B)/sprig/" + case + "/"
        node = lib.node_by_output(graph, prefix + output)
        self.assertEqual(node["outputs"], [prefix + output])
        self.assertEqual(node["cmds"][0]["cmd_args"][-3:], ["copy", "$(S)/sprig/" + case + "/" + source, prefix + output])
        self.assertIn("$(S)/sprig/" + case + "/" + source, node["inputs"])
        package = lib.node_by_output(graph, prefix + "sprig-" + case + ".pkg.fake")
        self.assertIn(node["uid"], package["deps"])
        self.assertIn(prefix + output, package["inputs"])
        self.assertIn(package["uid"], graph["result"])

    def test_unbound_bare_input_names_remain_literal_including_embedded_names(self):
        graph = self.graph(("bare",))
        for output, source in (("literal.h", "$UNBOUND"), ("bare.h", "$UNBOUND.h"),
                               ("interior.h", "prefix$UNBOUND.h"), ("context.h", "$UNBOUND.h")):
            with self.subTest(output=output):
                self.copied(graph, "bare", output, source)

    def test_unbound_braced_names_clear_and_bound_names_still_expand(self):
        for reverse in (False, True):
            graph = self.graph(("braced", "bound"), reverse)
            self.copied(graph, "braced", "braced.h", "raw.h")
            self.copied(graph, "braced", "interior.h", "prefixraw.h")
            for output in ("bare.h", "braced.h", "embedded.h", "chain.h"):
                self.copied(graph, "bound", output, "raw.h")

    def test_output_names_use_the_same_bare_and_braced_policy(self):
        graph = self.graph(("destinations",))
        for output in ("$UNBOUND.h", "braced.h", "chosen.h"):
            with self.subTest(output=output):
                self.copied(graph, "destinations", output, "raw.h")


if __name__ == "__main__":
    unittest.main()
