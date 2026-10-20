import unittest

import lib


class DockerImageTests(unittest.TestCase):
    def test_download_is_shared_by_test_runners(self):
        image = "docker://registry.example.invalid/team/image@sha256:1234567890abcdef=fixture"
        files = {
            f"{name}/ya.make": f"EXECTEST()\nRUN(check)\nDOCKER_IMAGE({image} {image})\nEND()\n"
            for name in ("first", "second")
        }
        graph = lib.make(files, "first", "second", "-t")
        download = lib.only_node_by_kind(graph, "IM")
        self.assertRegex(download["outputs"][0], r"^\$\(B\)/images/[0-9]+/image.tar$")
        args = download["cmds"][0]["cmd_args"]
        self.assertEqual(args[:2], [
            "$(YMAKE_PYTHON3)/bin/python3",
            "$(S)/build/scripts/fetch_from_docker_repo.py",
        ])
        self.assertEqual(args[-2:], [
            "--preloaded-path", "$(RESOURCE_ROOT)/docker/registry.example.invalid-1234567890ab/resource",
        ])
        self.assertEqual(download["inputs"], [])
        self.assertEqual(download["cmds"][0]["cwd"], "$(B)")
        for name in ("first", "second"):
            run = lib.node_by_output(graph, f"$(B)/{name}/test-results/exectest/meta.json")
            self.assertIn(download["uid"], run["deps"])

    def test_unused_test_images_do_not_download(self):
        graph = lib.make({
            "check/ya.make": "EXECTEST()\nRUN(check)\nDOCKER_IMAGE(docker://registry.test/image@sha256:1234567890abcdef=fixture)\nEND()\n",
        }, "check")
        self.assertFalse(any(n["kv"]["p"] == "IM" for n in graph["graph"]))


if __name__ == "__main__":
    unittest.main()
