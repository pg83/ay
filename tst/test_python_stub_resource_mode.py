import base64
import json
import unittest

import lib


MODULE = "bindings/specimen"
# Upstream CanHandle rejects the substring "conftest.py", including this stub.
STUB = "support/conftest.pyi"
RESCOMPILER = "$(B)/tools/rescompiler/rescompiler"


def fixture(body, macro="PY3_LIBRARY"):
    files = {
        f"{MODULE}/ya.make": (
            f"{macro}(typed_bundle)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
            f"PY_NAMESPACE(typed_contract)\n{body}END()\n"
        ),
        "library/cpp/resource/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
        "contrib/libs/python/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
    }
    for tool in ("rescompiler", "rescompressor", "archiver", "stub_writer"):
        lib.tool_program(files, "tools/" + tool, tool)
    return files


class PythonStubResourceModeTest(unittest.TestCase):
    def graph(self, files, platform="default-linux-x86_64", *flags):
        result = lib.make_process(files, MODULE, "--target-platform", platform, *flags)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn("module-failed", result.stderr)
        return json.loads(result.stdout)

    def assert_raw_stub(self, graph, input_path, extra_inputs=()):
        [packed] = [
            n for n in graph["graph"]
            if n["outputs"][0].startswith(f"$(B)/{MODULE}/")
            and n["outputs"][0].endswith("_raw.auxcpp")
        ]
        [output] = packed["outputs"]
        key = "resfs/file/py/typed_contract/" + STUB
        self.assertEqual(packed["cmds"][0]["cmd_args"], [
            RESCOMPILER, output, "-", f"resfs/src/{key}={MODULE}/{STUB}",
            input_path, "-" + key,
        ])
        self.assertCountEqual(packed["inputs"], [RESCOMPILER, input_path, *extra_inputs])
        tool = lib.node_by_output(graph, RESCOMPILER)
        self.assertIn(tool["uid"], packed["deps"])
        compiled = lib.node_by_output(graph, output + ".o")
        self.assertIn(packed["uid"], compiled["deps"])
        self.assertEqual(compiled["cmds"][0]["cmd_args"][-3:], ["-x", "c++", output])
        [archive] = [
            n for n in graph["graph"]
            if n["outputs"][0].startswith(f"$(B)/{MODULE}/")
            and n["outputs"][0].endswith(".global.a")
        ]
        self.assertIn(compiled["uid"], archive["deps"])
        self.assertIn(output + ".o", archive["inputs"])
        self.assertFalse(any(
            o.endswith(".yasm") or o.startswith("$(B)/tools/rorescompiler/")
            for n in graph["graph"] for o in n["outputs"]
        ))
        return packed

    def test_source_stubs_keep_raw_fallback_across_platforms_and_aux_config(self):
        for platform, flags in (
            ("default-linux-x86_64", ()),
            ("default-linux-x86_64", ("-DDISABLE_SEPARATE_AUX_CPP=yes",)),
            ("default-linux-aarch64", ()),
            ("default-darwin-x86_64", ()),
        ):
            with self.subTest(platform=platform, flags=flags):
                files = fixture(f"PY_SRCS({STUB} public.pyi)\n")
                files[f"{MODULE}/{STUB}"] = "value: int\n"
                files[f"{MODULE}/public.pyi"] = "name: str\n"
                graph = self.graph(files, platform, *flags)
                self.assert_raw_stub(graph, f"$(S)/{MODULE}/{STUB}")
                [public] = [
                    n for n in graph["graph"]
                    if f"$(S)/{MODULE}/public.pyi" in n["inputs"] and n["kv"]["p"] == "PY"
                ]
                args = public["cmds"][0]["cmd_args"]
                self.assertEqual(base64.b64decode(args[args.index("--keys") + 1]).decode(),
                                 "resfs/file/py/typed_contract/public.pyi")

    def test_python2_stub_uses_the_same_resource_mode(self):
        files = fixture(f"PY_SRCS({STUB})\n", macro="PY2_LIBRARY")
        files[f"{MODULE}/{STUB}"] = "value: int\n"
        self.assert_raw_stub(self.graph(files), f"$(S)/{MODULE}/{STUB}")

    def test_generated_stub_keeps_its_registered_producer_and_main_output(self):
        files = fixture(
            f"RUN_PROGRAM(tools/stub_writer OUT_NOAUTO manifest.txt {STUB})\n"
            f"PY_SRCS({STUB})\n"
        )
        graph = self.graph(files)
        main = f"$(B)/{MODULE}/manifest.txt"
        packed = self.assert_raw_stub(graph, f"$(B)/{MODULE}/{STUB}", [main])
        producer = lib.node_by_output(graph, main)
        self.assertIn(producer["uid"], packed["deps"])
        self.assertNotIn(main, packed["cmds"][0]["cmd_args"])

    def test_shared_python2_and_python3_module_keeps_distinct_aux_producers(self):
        files = fixture(f"PY_SRCS({STUB})\n", macro="PY23_LIBRARY")
        files[f"{MODULE}/{STUB}"] = "value: int\n"
        files["bindings/consumer/ya.make"] = (
            f"PROGRAM()\n{lib.BARE_MODULE}USE_PYTHON2()\nPEERDIR({MODULE})\nEND()\n"
        )
        graph = self.graph(files, "default-linux-x86_64", "bindings/consumer")
        packed = [n for n in graph["graph"] if n["outputs"][0].endswith("_raw.auxcpp")]
        self.assertEqual(len(packed), 2)
        self.assertEqual(len({n["outputs"][0] for n in packed}), 2)
        self.assertEqual(len({n["uid"] for n in packed}), 2)
        for suffix, obj_suffix in (
            ("libpytyped_bundle.global.a", ".py2.o"),
            ("libpy3typed_bundle.global.a", ".py3.o"),
        ):
            archive = lib.node_by_output(graph, f"$(B)/{MODULE}/{suffix}")
            [obj] = [p for p in archive["inputs"] if p.endswith("_raw.auxcpp" + obj_suffix)]
            compiled = lib.node_by_output(graph, obj)
            producer = lib.node_by_output(graph, obj.removesuffix(obj_suffix))
            self.assertIn(compiled["uid"], archive["deps"])
            self.assertIn(producer["uid"], compiled["deps"])
            self.assertIn(producer, packed)
            self.assertEqual(producer["cmds"][0]["cmd_args"][-2:], [
                f"$(S)/{MODULE}/{STUB}", "-resfs/file/py/typed_contract/" + STUB,
            ])

    def test_explicit_compressed_resource_still_uses_rorescompiler(self):
        files = fixture(f"RESOURCE({STUB} typed_contract)\n")
        files[f"{MODULE}/{STUB}"] = "value: int\n"
        lib.tool_program(files, "tools/rorescompiler", "rorescompiler")
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = self.graph(files)
        tool = lib.node_by_output(graph, "$(B)/tools/rorescompiler/rorescompiler")
        [aux] = [n for n in graph["graph"] if n["outputs"][0].endswith(".auxcpp")]
        self.assertIn(tool["uid"], aux["deps"])
        self.assertEqual(aux["cmds"][0]["cmd_args"][0], tool["outputs"][0])
        compiled = lib.node_by_output(graph, aux["outputs"][0] + ".o")
        self.assertIn(aux["uid"], compiled["deps"])
        [yasm] = [n for n in graph["graph"] if n["outputs"][0].endswith(".yasm")]
        compressor = lib.node_by_output(graph, "$(B)/tools/rescompressor/rescompressor")
        self.assertIn(compressor["uid"], yasm["deps"])
        self.assertIn(f"$(S)/{MODULE}/{STUB}", yasm["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
