import unittest

import lib


def fixture():
    return {
        "app/fuzz/ya.make": f"FUZZ()\n{lib.BARE_MODULE}SRCS(main.cpp)\nFUZZ_OPTS(-runs=12)\nEND()\n",
        "app/fuzz/main.cpp": "int main(){return 0;}\n",
        "library/cpp/testing/nofuzz/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
        "contrib/libs/libfuzzer/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
    }


class FuzzMetadataTest(unittest.TestCase):
    def test_corpus_metadata_is_a_related_path_even_before_it_exists(self):
        for present in (False, True):
            with self.subTest(present=present):
                files = fixture()
                if present:
                    files["fuzzing/app/fuzz/corpus.json"] = "{}"
                graph = lib.make(files, "app/fuzz", "-t")
                run = lib.node_by_output(graph, "$(B)/app/fuzz/test-results/fuzz/meta.json")
                self.assertEqual(run["inputs"], ["$(S)/app/fuzz", "$(S)/fuzzing/app/fuzz/corpus.json"])
                command = run["cmds"][0]["cmd_args"]
                self.assertEqual([command[i + 1] for i, arg in enumerate(command) if arg == "--test-related-path"], ["$(S)/fuzzing/app/fuzz/corpus.json", "$(S)/app/fuzz"])

    def test_corpus_metadata_path_can_be_overridden(self):
        for corpus, expected in (("other/corpus.json", "$(S)/other/corpus.json"), ("/external/corpus.json", "/external/corpus.json")):
            with self.subTest(corpus=corpus):
                graph = lib.make(fixture(), "app/fuzz", "-t", env={"YA_TEST_CORPUS_DATA_PATH": corpus})
                run = lib.node_by_output(graph, "$(B)/app/fuzz/test-results/fuzz/meta.json")
                self.assertIn(expected, run["inputs"])
                self.assertNotIn("$(S)/fuzzing/app/fuzz/corpus.json", run["inputs"])

    def test_dist_fuzzer_truncates_logs_before_passing_fuzzer_options(self):
        for dist in (False, True):
            with self.subTest(dist=dist):
                graph = lib.make(fixture(), "app/fuzz", "-t", *(["--dist"] if dist else []))
                run = lib.node_by_output(graph, "$(B)/app/fuzz/test-results/fuzz/meta.json")
                command = run["cmds"][0]["cmd_args"]
                self.assertEqual("--truncate-logs" in command, dist)
                self.assertIn("--fuzz-opts=-runs=12", command)
                if dist:
                    self.assertLess(command.index("--truncate-logs"), command.index("--fuzz-opts=-runs=12"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
