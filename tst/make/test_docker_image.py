import unittest

from tst import lib


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

    def test_image_paths_match_upstream_cityhash_for_all_tag_lengths(self):
        # Values come from util/digest/city.cpp at upstream d02375f8,
        # independently evaluated on the same ASCII tags.
        cases = (
            (0, 11160318154034397263),
            (1, 2603192927274642682),
            (3, 4220206313085259313),
            (4, 17823623939509273229),
            (8, 4864636163090995991),
            (9, 7406163328168016081),
            (16, 13242281798114038465),
            (17, 237482408704357350),
            (32, 4401604186254454218),
            (33, 6149775935917593809),
            (64, 10870159159245104547),
            (65, 13407693885279354766),
            (127, 16519838804566824773),
            (128, 13102153355302731846),
            (129, 8716585674679283391),
            (256, 14092306236991204336),
        )
        for length, upstream_hash in cases:
            with self.subTest(length=length):
                tag = "".join(chr(97 + i % 26) for i in range(length))
                image = f"docker://registry.test/image@sha256:1234567890abcdef={tag}"
                files = {"app/ya.make": f"EXECTEST()\nRUN(check)\nDOCKER_IMAGE({image})\nEND()\n"}
                graph = lib.make(files, "app", "-t")
                node = lib.node_by_output(graph, f"$(B)/images/{upstream_hash}/image.tar")
                args = node["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--tag") + 1], tag)
                run = lib.node_by_output(graph, "$(B)/app/test-results/exectest/meta.json")
                self.assertIn(node["uid"], run["deps"])


if __name__ == "__main__":
    unittest.main()
