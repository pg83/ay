import unittest

from tst import lib
from tst.make.test_yql import udf_fixture


BARE = lib.BARE_MODULE
X86_64 = ("--target-platform", "default-linux-x86_64")


def fixture():
    files = udf_fixture()
    files["build/scripts/rodata2asm.py"] = ""
    lib.tool_program(files, "contrib/tools/yasm", "yasm")
    return files


class RodataContextsTest(unittest.TestCase):
    def assert_rodata(self, graph, directory, source, suffix, producer=None):
        root = f"$(B)/{directory}/"
        stem = root + ("_/" if "/" in source else "") + source
        node = lib.node_by_output(graph, stem + suffix + ".o")
        self.assertEqual(node["outputs"], [stem + ".asm", stem + suffix + ".o"])
        self.assertEqual(node["cmds"][1]["cmd_args"][-3:], ["-o", stem + suffix + ".o", stem + ".asm"])
        if producer is not None:
            self.assertIn(producer["uid"], node["deps"])
            self.assertIn(producer["outputs"][0], node["inputs"])
        return node

    def test_udf_static_variants_keep_distinct_archives_and_rodata_objects(self):
        files = fixture()
        files.update({
            "udf/ya.make": f"YQL_UDF(named)\n{BARE}UDF_NO_PROBE()\nARCHIVE_ASM(NAME Blob data.bin)\nEND()\n",
            "udf/data.bin": "binary",
        })
        graph = lib.make(files, "udf", *X86_64)
        for suffix, library in ((".udfs", "libnamed.a"), (".udfs.abi_current", "yql-abi-current-named.a")):
            with self.subTest(suffix=suffix):
                source = "Blob" + suffix + ".rodata"
                archive = lib.node_by_output(graph, "$(B)/udf/" + source)
                self.assertEqual(archive["cmds"][0]["cmd_args"][-2:], ["-o", "$(B)/udf/" + source])
                rodata = self.assert_rodata(graph, "udf", source, suffix, archive)
                self.assertIn("Blob" + suffix, rodata["cmds"][0]["cmd_args"])
                libnode = lib.node_by_output(graph, "$(B)/udf/" + library)
                self.assertIn(rodata["uid"], libnode["deps"])
                self.assertTrue(set(rodata["outputs"]).issubset(libnode["inputs"]))

    def test_source_rodata_and_archive_names_follow_both_abi_consumers(self):
        files = fixture()
        files.update({
            "stable/ya.make": f"PROGRAM()\n{BARE}PEERDIR(shared)\nEND()\n",
            "current/ya.make": f"PROGRAM()\n{BARE}YQL_CURRENT_ABI_VERSION()\nPEERDIR(shared)\nEND()\n",
            "shared/ya.make": f'YQL_LIBRARY()\n{BARE}IF (MODULE_TAG == "YQL_ABI_CURRENT")\nSRCS(current/data.rodata)\nELSE()\nSRCS(stable/data.rodata)\nENDIF()\nARCHIVE_ASM(NAME Blob x.bin)\nEND()\n',
            "shared/stable/data.rodata": "one",
            "shared/current/data.rodata": "two",
            "shared/x.bin": "three",
        })
        for variant, pic in (("stable", False), ("current", False), ("current", True)):
            with self.subTest(variant=variant, pic=pic):
                graph = lib.make(files, variant, *X86_64, *(["-DPIC=yes"] if pic else []))
                suffix = ".abi_" + variant
                object_suffix = suffix + (".pic" if pic else "")
                archive = lib.node_by_output(graph, f"$(B)/shared/Blob{suffix}.rodata")
                generated = self.assert_rodata(graph, "shared", "Blob" + suffix + ".rodata", object_suffix, archive)
                inputs = [generated]
                for source in (variant + "/data.rodata",):
                    node = self.assert_rodata(graph, "shared", source, object_suffix)
                    self.assertIn("$(S)/shared/" + source, node["inputs"])
                    inputs.append(node)
                library = lib.node_by_output(graph, f"$(B)/shared/yql-abi-{variant}-shared.a")
                for node in inputs:
                    self.assertIn(node["uid"], library["deps"])
                    self.assertTrue(set(node["outputs"]).issubset(library["inputs"]))

    def test_python_native_variant_uses_its_object_suffix(self):
        files = fixture()
        files.update({
            "mod/ya.make": f"PY23_NATIVE_LIBRARY()\n{BARE}NO_PYTHON_INCLUDES()\nARCHIVE_ASM(NAME Blob data.bin)\nSRCS(data.rodata)\nEND()\n",
            "mod/data.bin": "binary",
            "mod/data.rodata": "source",
            "contrib/libs/python/ya.make": f"LIBRARY()\n{BARE}END()\n",
        })
        graph = lib.make(files, "mod", *X86_64)
        archive = lib.node_by_output(graph, "$(B)/mod/Blob.py3.rodata")
        nodes = [self.assert_rodata(graph, "mod", "Blob.py3.rodata", ".py3", archive),
                 self.assert_rodata(graph, "mod", "data.rodata", ".py3")]
        library = lib.node_by_output(graph, "$(B)/mod/libpy3cmod.a")
        for node in nodes:
            self.assertIn(node["uid"], library["deps"])
            self.assertTrue(set(node["outputs"]).issubset(library["inputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
