import json
import unittest

import lib
from resource_dependencies import fetches, fetched_uri


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


def files_for_entry(entry, macro):
    raw = '{"by_platform":{"linux":' + entry + ',"linux-aarch64":' + entry + '}}'
    files = files_for_pairs([])
    files["res/bundle.json"] = raw
    if macro == "set-uri":
        files["lib/ya.make"] = (
            "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
            "SET_RESOURCE_URI_FROM_JSON(URI bundle.json)\nCFLAGS(-DPROBE=${URI})\n"
            "SRCS(main.cpp)\nEND()\n"
        )
        files["lib/bundle.json"] = raw
    return files


class ResourceJSONOrderTest(unittest.TestCase):
    def test_json_entry_fields_follow_native_presence_and_numeric_rules(self):
        cases = [
            ('{"uri":"sbr:1","strip_prefix":0}', "sbr:1", 0, None),
            ('{"uri":"sbr:1","strip_prefix":1}', "sbr:1", 1, None),
            ('{"uri":"sbr:1","strip_prefix":1.0}', "sbr:1", 1, None),
            ('{"uri":"sbr:1","strip_prefix":1e0}', "sbr:1", 1, None),
            ('{"uri":"sbr:1","strip_prefix":1.5}', None, None, "should be integral"),
            ('{"uri":"sbr:1","strip_prefix":-1}', None, None, "should be integral"),
            ('{"uri":"sbr:1","strip_prefix":null}', None, None, "should be integral"),
            ('{"uri":"sbr:1","strip_prefix":65535}', "sbr:1", 65535, None),
            ('{"uri":"sbr:1","strip_prefix":65536}', None, None, "value 65536 is too big"),
            ('{}', None, None, "uri is not found"),
            ('{"uri":null}', None, None, "uri is not found"),
            ('{"uri":""}', "", 0, None),
            ('{"uri":"sbr:1","strip_prefix":null,"strip_prefix":1}', "sbr:1", 1, None),
            ('{"uri":"sbr:1","strip_prefix":1,"strip_prefix":null}', None, None, "should be integral"),
            ('{"uri":null,"uri":"sbr:1"}', "sbr:1", 0, None),
            ('{"uri":"sbr:1","uri":null}', None, None, "uri is not found"),
        ]
        for macro in ("bundle", "set-uri"):
            for entry, uri, prefix, diagnostic in cases:
                with self.subTest(macro=macro, entry=entry):
                    result = lib.make_process(files_for_entry(entry, macro), "lib")
                    if diagnostic is not None:
                        self.assertEqual(result.returncode, 1, result.stderr)
                        self.assertIn(diagnostic, result.stderr)
                        self.assertEqual(result.stdout, "")
                        continue
                    self.assertEqual(result.returncode, 0, result.stderr)
                    graph = json.loads(result.stdout)
                    compiled = lib.node_by_output(graph, "$(B)/lib/main.cpp.o")
                    if macro == "set-uri":
                        self.assertIn("-DPROBE=" + uri, compiled["cmds"][0]["cmd_args"])
                        self.assertEqual(fetches(graph), {})
                    else:
                        producer = lib.node_by_output(graph, "$(B)/resources/T")
                        self.assertIn(producer["uid"], compiled["deps"])
                        self.assertIn("-DPROBE=$(B)/resources/T", compiled["cmds"][0]["cmd_args"])
                        descriptor = json.loads(fetches(graph)[producer["outputs"][0]][2])
                        self.assertEqual(descriptor["pattern"], "T")
                        self.assertEqual([(item["platform"], item.get("strip_prefix", 0))
                                          for item in descriptor["resources"]], [
                            ("LINUX", prefix), ("LINUX-AARCH64", prefix),
                        ])
                        if uri:
                            self.assertEqual(fetched_uri(graph, "T"), uri)

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
