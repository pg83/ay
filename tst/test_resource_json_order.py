import json
import unittest

import lib
from resource_dependencies import fetched_uri


SAME_BUCKET = [("linux", "sbr:100"), ("linuX", "sbr:101")]
GROWTH = [(key, f"sbr:{200 + index}") for index, key in enumerate(
    ("linux", "linuX", "LinUX", "LiNUx", "LiNUX", "LINuX", "LINUx", "LINUX"))]


def files_for_pairs(pairs):
    mapping = ", ".join(json.dumps(key) + ": " + json.dumps({"uri": uri}) for key, uri in pairs)
    return {
        "res/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(T bundle.json)\nEND()\n",
        "res/bundle.json": '{"by_platform": {' + mapping + "}}",
        "lib/ya.make": "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(res)\nCFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n",
        "lib/main.cpp": "int main(){return 0;}\n",
    }


class ResourceJSONOrderTest(unittest.TestCase):
    def test_json_resource_macros_reject_null_and_empty_platform_mappings(self):
        for macro in ("DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(T bundle.json)",
                      "SET_RESOURCE_URI_FROM_JSON(URI bundle.json)\nDECLARE_EXTERNAL_RESOURCE(T ${URI})"):
            for mapping in (None, {}):
                with self.subTest(macro=macro, mapping=mapping):
                    files = files_for_pairs([])
                    files["res/ya.make"] = (
                        f"RESOURCES_LIBRARY()\n{macro}\nEND()\n"
                    )
                    files["res/bundle.json"] = json.dumps({"by_platform": mapping})
                    result = lib.make_process(files, "lib")
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn("platform mapping is empty", result.stderr)
                    self.assertEqual(result.stdout, "")

    def test_json_bundle_rejects_non_object_mapping_and_non_string_uri(self):
        for mapping, diagnostic in (([], "resource JSON mapping must be an object"),
                                    ({"linux": {"uri": 17}}, "cannot unmarshal number")):
            with self.subTest(mapping=mapping):
                files = files_for_pairs([])
                files["res/bundle.json"] = json.dumps({"by_platform": mapping})
                result = lib.make_process(files, "lib")
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertIn(diagnostic, result.stderr)
                self.assertEqual(result.stdout, "")

    def check_pairs(self, pairs, expected):
        graph = lib.make(files_for_pairs(pairs), "lib")
        self.assertEqual(fetched_uri(graph, "T"), expected)
        producer = lib.node_by_output(graph, "$(B)/resources/T")
        compiled = lib.node_by_output(graph, "$(B)/lib/main.cpp.o")
        self.assertIn(producer["uid"], compiled["deps"])
        self.assertIn("-DPROBE=$(B)/resources/T", compiled["cmds"][0]["cmd_args"])

    def test_alias_collisions_follow_native_hash_iteration(self):
        original = [("linux", "sbr:170"), ("linux-x86_64", "sbr:171"), ("linux-aarch64", "sbr:172")]
        for name, pairs, expected in (
            ("go-order", original, "sbr:171"),
            ("reverse-order", original[::-1], "sbr:171"),
            ("same-bucket-forward", SAME_BUCKET, "sbr:101"),
            ("same-bucket-reverse", SAME_BUCKET[::-1], "sbr:100"),
            ("rehash-forward", GROWTH, "sbr:205"),
            ("rehash-reverse", GROWTH[::-1], "sbr:206"),
        ):
            with self.subTest(name=name):
                self.check_pairs(pairs, expected)

    def test_duplicate_keys_keep_the_existing_slot_and_bucket_count(self):
        for name, pairs, expected in (
            ("duplicate-exact", SAME_BUCKET + [("linux", "sbr:102")], "sbr:101"),
            ("duplicate-growth-boundary", GROWTH[:7] + [("linux", "sbr:299")], "sbr:206"),
        ):
            with self.subTest(name=name):
                self.check_pairs(pairs, expected)


if __name__ == "__main__":
    unittest.main()
