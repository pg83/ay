import base64
import hashlib
import unittest

from tst import lib
from tst.make.test_python_srcs import python_base
from tst.make.test_resource_objcopy import section


def fixture(module, count, version=3, switches="", calls=None):
    files = python_base()
    lib.tool_program(files, "tools/py2cc", "py2cc")
    names = [f"part_{i:04d}.py" for i in range(count)]
    files.update({f"{module}/{name}": "" for name in names})
    if calls is None:
        calls = [names]
    files[f"{module}/ya.make"] = (
        f"PY{version}_LIBRARY(bundle)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
        "NO_EXTENDED_SOURCE_SEARCH()\n" + switches
        + "".join("PY_SRCS(TOP_LEVEL " + " ".join(call) + ")\n" for call in calls)
        + "END()\n"
    )
    return files, names


def packs(graph, module):
    return [n for n in graph["graph"]
            if n["outputs"][0].startswith(f"$(B)/{module}/objcopy_")]


class PythonResourcePartitionsTest(unittest.TestCase):
    def assert_payloads(self, graph, module, names, version, with_py=True, with_pyc=False):
        objects = packs(graph, module)
        payloads = []
        for node in objects:
            args = node["cmds"][0]["cmd_args"]
            inputs = section(args, "--inputs")
            keys = [base64.b64decode(key).decode() for key in section(args, "--keys")]
            payloads.extend(zip(inputs, keys))
            for tool in ("rescompiler", "rescompressor"):
                producer = lib.node_by_output(graph, f"$(B)/tools/{tool}/{tool}")
                self.assertIn(producer["uid"], node["deps"])
            for path in inputs:
                self.assertIn(path, node["inputs"])
                if path.startswith("$(B)"):
                    producer = lib.node_by_output(graph, path)
                    self.assertIn(producer["uid"], node["deps"])
            # The outer macro boundary is in original source rows, including
            # both the source and bytecode entries belonging to each row.
            rows = {int(path.rsplit("part_", 1)[1][:4]) // 1024 for path in inputs}
            self.assertLessEqual(len(rows), 1)
        expected = []
        for name in names:
            stem = name.removesuffix(".py")
            if with_py:
                key = f"resfs/file/py/{name}" if version == 3 else f"/py_modules/{stem}"
                expected.append((f"$(S)/{module}/{name}", key))
            if with_pyc:
                ext = ".yapyc3" if version == 3 else ".yapyc"
                key = f"resfs/file/py/{name}{ext}" if version == 3 else f"/py_code/{stem}"
                expected.append((f"$(B)/{module}/{name}{ext}", key))
        self.assertEqual(payloads, expected)
        prefix = "py3" if version == 3 else "py"
        archive = lib.node_by_output(graph, f"$(B)/{module}/lib{prefix}bundle.global.a")
        self.assertEqual([path for path in archive["inputs"] if "/objcopy_" in path],
                         [node["outputs"][0] for node in objects])
        for node in objects:
            self.assertIn(node["uid"], archive["deps"])
        return objects

    def test_native_python_source_row_boundary(self):
        # Native pinned PY_SRCS + RESOURCE_FILES + resource_handler command
        # graphs have 69, 69, 70 objects respectively (15 source rows per
        # full inner batch for these exact arguments).
        for case, count, object_count in (("before", 1023, 69), ("at", 1024, 69),
                                           ("after", 1025, 70)):
            with self.subTest(case=case):
                module = "amber/" + case
                files, names = fixture(module, count, switches="ENABLE(PYBUILD_NO_PYC)\n")
                graph = lib.make(files, module)
                objects = self.assert_payloads(graph, module, names, 3)
                self.assertEqual(len(objects), object_count)
                if case == "after":
                    # Exact final outputs from the native command graph.
                    self.assertEqual([n["outputs"][0] for n in objects[-2:]], [
                        "$(B)/amber/after/objcopy_fe793b0f47843b65bbfe57716f.o",
                        "$(B)/amber/after/objcopy_eb16bfa4ca083908756a308c96.o",
                    ])

    def test_bytecode_modes_keep_both_entries_in_each_original_row(self):
        for version, switches, with_py in ((3, "", True), (2, "", True),
                                           (3, "ENABLE(PYBUILD_NO_PY)\n", False),
                                           (2, "ENABLE(PYBUILD_NO_PY)\n", False)):
            with self.subTest(version=version, with_py=with_py):
                module = "amber/bytecode"
                files, names = fixture(module, 1025, version, switches)
                graph = lib.make(files, module)
                self.assert_payloads(graph, module, names, version, with_py, True)

    def test_separate_source_calls_keep_their_own_partial_batch(self):
        module = "amber/separate"
        names = [f"part_{i:04d}.py" for i in range(17)]
        files, _ = fixture(module, len(names), switches="ENABLE(PYBUILD_NO_PYC)\n",
                           calls=[names[:16], names[16:]])
        graph = lib.make(files, module)
        objects = self.assert_payloads(graph, module, names, 3)
        self.assertEqual([len(section(n["cmds"][0]["cmd_args"], "--inputs"))
                          for n in objects], [15, 1, 1])

    def test_disabled_payloads_leave_no_empty_objects(self):
        module = "amber/disabled"
        files, _ = fixture(module, 1025,
                           switches="ENABLE(PYBUILD_NO_PY PYBUILD_NO_PYC)\n")
        self.assertEqual(packs(lib.make(files, module), module), [])

    def test_direct_resource_files_has_no_python_row_cap(self):
        module = "amber/unbounded"
        files, names = fixture(module, 1025)
        args = " ".join(f"DEST py/{name} {name}" for name in names)
        files[f"{module}/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}RESOURCE_FILES(DONT_COMPRESS {args})\nEND()\n"
        )
        objects = packs(lib.make(files, module), module)
        self.assertEqual(len(objects), 69)
        self.assertEqual(section(objects[-1]["cmds"][0]["cmd_args"], "--inputs"),
                         [f"$(S)/{module}/{name}" for name in names[1020:]])

    def test_inline_budget_counts_utf8_bytes_and_flushes_after_item(self):
        module = "amber/inline"
        for cost in (7999, 8000, 8001):
            with self.subTest(cost=cost):
                # C++ accounts for the original inline string's UTF-8 bytes,
                # plus 200 bytes, and flushes after the item reaching 8000.
                value = "key=é" + "x" * (cost - 200 - len("key=é".encode()))
                files = python_base()
                files[f"{module}/ya.make"] = (
                    f"LIBRARY()\n{lib.BARE_MODULE}"
                    f"RESOURCE(DONT_COMPRESS - {value} - tail=value)\nEND()\n"
                )
                objects = packs(lib.make(files, module), module)
                batches = [[value, "tail=value"]] if cost < 8000 else [[value], ["tail=value"]]
                self.assertEqual([section(n["cmds"][0]["cmd_args"], "--kvs")
                                  for n in objects], batches)
                for node, batch in zip(objects, batches):
                    digest = hashlib.md5(
                        ",".join(sorted([*batch, "$S/" + module])).encode()
                    ).hexdigest()[:26]
                    self.assertEqual(node["outputs"], [f"$(B)/{module}/objcopy_{digest}.o"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
