import base64
import hashlib
import unittest

from tst import lib
from tst.make.test_package_global_inputs import files
from tst.make.test_resource_objcopy import section


def fixture(module="DLL", prelude="", body="", expected="DLL"):
    sources = files(module=module)
    sources["meadow/tool/ya.make"] = prelude + sources["meadow/tool/ya.make"].replace(
        "RESOURCE(payload.txt meadow_payload)", body + "INCLUDE(resource.inc)")
    sources["meadow/tool/resource.inc"] = (
        f'IF(MODULE_TAG == "{expected}")\nRESOURCE(payload.txt meadow_payload)\n'
        'ELSE()\nRESOURCE(payload.txt wrong_header_tag)\nENDIF()\n')
    return sources


class DllResourceModuleTagTest(unittest.TestCase):
    def check_resource(self, graph, tag, library=False, standalone=False):
        objects = [node for node in graph["graph"]
                   if any(output.startswith("$(B)/meadow/tool/objcopy_") for output in node["outputs"])]
        self.assertEqual(len(objects), 1)
        packed = objects[0]
        argv = packed["cmds"][0]["cmd_args"]
        self.assertEqual(section(argv, "--keys"), [base64.b64encode(b"meadow_payload").decode()])
        self.assertEqual(section(argv, "--inputs"), ["$(S)/meadow/tool/payload.txt"])
        # Upstream resource_handler/packer.h includes the effective module tag.
        seed = ",".join(sorted(("payload.txt", "bWVhZG93X3BheWxvYWQ=", "$S/meadow/tool"))) + tag
        output = "$(B)/meadow/tool/objcopy_" + hashlib.md5(seed.encode()).hexdigest()[:26] + ".o"
        self.assertEqual(packed["outputs"], [output])
        if standalone:
            self.assertTrue(any(packed["uid"] in node["deps"] and output in node["inputs"]
                                and node["kv"].get("p") == "LD" for node in graph["graph"]))
            return
        package = lib.node_by_output(graph, "$(B)/meadow/parcel/parcel.final.pkg.fake")
        if library:
            archive = lib.node_by_output(graph, "$(B)/meadow/tool/libutility.global.a")
            self.assertIn(packed["uid"], archive["deps"])
            self.assertIn(archive["uid"], package["deps"])
        else:
            self.assertIn(output, package["inputs"])
            self.assertIn(packed["uid"], package["deps"])
        ids = {node["uid"] for node in graph["graph"]}
        self.assertLessEqual(set(graph["result"]), ids)
        for node in graph["graph"]:
            self.assertLessEqual(set(node["deps"]), ids)
        self.assertIn(package["uid"], graph["result"])

    def test_dll_header_sets_tag_before_shared_include_and_overrides_directory_value(self):
        for prelude in ("", "SET(MODULE_TAG stale)\n"):
            for targets in (("meadow/parcel", "meadow/tool"), ("meadow/tool", "meadow/parcel")):
                with self.subTest(prelude=prelude, targets=targets):
                    self.check_resource(lib.make(fixture(prelude=prelude), *targets, "--sandboxing"), "DLL")

    def test_body_set_overrides_header_default(self):
        self.check_resource(lib.make(fixture(body="SET(MODULE_TAG cedar)\n", expected="cedar"),
                                     "meadow/tool", "--sandboxing"), "cedar", standalone=True)

    def test_ordinary_program_and_library_keep_their_own_header_context(self):
        for module in ("PROGRAM", "LIBRARY"):
            with self.subTest(module=module):
                self.check_resource(lib.make(fixture(module, expected=""), "meadow/parcel", "--sandboxing"),
                                    "", library=module == "LIBRARY")

    def test_python_dll_keeps_derived_header_tag(self):
        sources = fixture("DLL", expected="PY2_NATIVE")
        sources["meadow/tool/ya.make"] = sources["meadow/tool/ya.make"].replace("DLL(utility)", "PY2MODULE(utility)")
        self.check_resource(lib.make(sources, "meadow/tool", "--sandboxing"), "PY2_NATIVE", standalone=True)


if __name__ == "__main__":
    unittest.main(verbosity=2)
