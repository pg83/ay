"""Java test image metadata reaches the existing shared Docker dependency path."""
import unittest

from tst import lib
from tst.make.test_java_tests import junit_tree, test_nodes

IMAGE = "docker://registry.example.invalid/orchard/database@sha256:" + "0123456789abcdef" * 4 + "=orchard:7"
OTHER = "docker://registry.example.invalid/orchard/cache@sha256:" + "abcdef0123456789" * 4 + "=cache:2"


def fixture(body="DOCKER_IMAGE(" + IMAGE + ")", kind="JUNIT5"):
    files = junit_tree(kind + "()\n" + body + "\nJAVA_SRCS(TestLeaf.java)\nEND()\n", **{
        "devtools/junit-runner/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
        "devtools/junit6-runner/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
        "devtools/jtest-annotations/junit4/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
        "devtools/jtest-annotations/junit6/ya.make": "JAVA_LIBRARY_NON_SPLIT()\nEND()\n",
        "proj/sub/ut/TestLeaf.java": "class TestLeaf {}\n",
    })
    return files


class JavaDockerImageTest(unittest.TestCase):
    def test_junit_generations_use_existing_image_producer_and_dependency(self):
        for kind in ("JTEST", "JUNIT5", "JUNIT6"):
            with self.subTest(kind=kind):
                graph = lib.make(fixture(kind=kind), "proj/sub/ut", "-t", "--sandboxing")
                image = lib.only_node_by_kind(graph, "IM")
                suite = test_nodes(graph)["proj/sub/ut/sub-ut"]
                self.assertIn(image["uid"], suite["deps"])
                self.assertEqual(image["inputs"], [])
                args = image["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--link") + 1], IMAGE.rsplit("=", 1)[0])
                self.assertEqual(args[args.index("--tag") + 1], "orchard:7")
                self.assertIn(lib.node_by_output(graph, "$(B)/proj/sub/ut/proj-sub-ut.jar")["uid"], suite["deps"])

    def test_source_variable_append_replacement_and_duplicates_reach_final_metadata(self):
        for body, tags in (
            ("SET(IMAGE " + IMAGE + ")\nDOCKER_IMAGE($IMAGE $IMAGE)\nDOCKER_IMAGE(" + OTHER + ")", {"orchard:7", "cache:2"}),
            ("DOCKER_IMAGE(" + IMAGE + ")\nSET(DOCKER_IMAGES_VALUE " + OTHER + ")", {"cache:2"}),
            ("SET_APPEND(DOCKER_IMAGES_VALUE " + IMAGE + ")", {"orchard:7"}),
            ("DOCKER_IMAGE(orchard:7=" + IMAGE.rsplit("=", 1)[0] + " " + IMAGE + ")", {"orchard:7"}),
            ('SET(DOCKER_IMAGES_VALUE "" \'\')', set()),
        ):
            with self.subTest(body=body):
                graph = lib.make(fixture(body), "proj/sub/ut", "-t", "--sandboxing")
                images = [n for n in graph["graph"] if n["kv"].get("p") == "IM"]
                suite = test_nodes(graph)["proj/sub/ut/sub-ut"]
                self.assertEqual({n["cmds"][0]["cmd_args"][n["cmds"][0]["cmd_args"].index("--tag") + 1] for n in images}, tags)
                self.assertEqual(len(images), len(tags))
                self.assertTrue(all(n["uid"] in suite["deps"] for n in images))

    def test_quoted_empty_scalar_binding_does_not_request_an_image(self):
        graph = lib.make(fixture(""), "proj/sub/ut", "-t", "--sandboxing", '-DDOCKER_IMAGES_VALUE=""')
        self.assertFalse(any(n["kv"].get("p") == "IM" for n in graph["graph"]))

    def test_unused_images_stay_absent_and_shared_images_have_one_producer(self):
        files = fixture()
        files["check/ya.make"] = "EXECTEST()\nRUN(echo)\nDOCKER_IMAGE(" + IMAGE + ")\nEND()\n"
        graph = lib.make(files, "proj/sub/ut", "check", "-t", "--sandboxing")
        image = lib.only_node_by_kind(graph, "IM")
        for suite in (test_nodes(graph)["proj/sub/ut/sub-ut"], lib.node_by_output(graph, "$(B)/check/test-results/exectest/meta.json")):
            self.assertIn(image["uid"], suite["deps"])
        unused = lib.make(fixture(), "proj/sub/ut", "--sandboxing")
        self.assertFalse(any(n["kv"].get("p") == "IM" for n in unused["graph"]))
        no_image = lib.make(fixture(""), "proj/sub/ut", "-t", "--sandboxing")
        self.assertFalse(any(n["kv"].get("p") == "IM" for n in no_image["graph"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
