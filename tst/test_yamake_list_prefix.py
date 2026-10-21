import unittest

import lib


class ListPrefixTest(unittest.TestCase):
    def test_missing_fixed_arguments_report_a_configuration_error(self):
        result = lib.make_process({"p/ya.make": "UNION()\nSET_LIST_PRE_SUF(OUT pre)\nEND()\n"}, "p")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SET_LIST_PRE_SUF expects an output, prefix and suffix", result.stderr)

    def test_prefix_and_suffix_are_text_fragments(self):
        for prefix, suffix in (("${ARCADIA_ROOT}/", "/data.txt"), ("${EMPTY}", "${EMPTY}")):
            with self.subTest(prefix=prefix, suffix=suffix):
                files = {
                    "p/ya.make": "UNION()\nSET(EMPTY)\n"
                        f"SET_LIST_PRE_SUF(INPUTS {prefix} {suffix} payload/one payload/two)\n"
                        "RUN_PROGRAM(tools/gen IN_NOPARSE ${INPUTS} OUT_NOAUTO main.bin)\nEND()\n",
                    "payload/one/data.txt": "one\n", "payload/two/data.txt": "two\n",
                    "payload/one": "one\n", "payload/two": "two\n",
                }
                if "ARCADIA_ROOT" in prefix:
                    del files["payload/one"]
                    del files["payload/two"]
                else:
                    del files["payload/one/data.txt"]
                    del files["payload/two/data.txt"]
                lib.tool_program(files, "tools/gen", "gen")
                graph = lib.make(files, "p")
                node = lib.node_by_output(graph, "$(B)/p/main.bin")
                tail = "/data.txt" if suffix == "/data.txt" else ""
                self.assertCountEqual(node["inputs"], [
                    "$(B)/tools/gen/gen", "$(S)/payload/one" + tail, "$(S)/payload/two" + tail,
                ])


if __name__ == "__main__":
    unittest.main(verbosity=2)
