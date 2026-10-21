import unittest

import lib


BARE = lib.BARE_MODULE
X86_64 = ("--target-platform", "default-linux-x86_64")


def fixture(body):
    files = {
        "mod/ya.make": f"LIBRARY()\n{BARE}CFLAGS(-DNOT_BPF)\nADDINCL(inc)\n{body}END()\n",
        "mod/a.c": '#include <api.h>\n',
        "mod/sub/a.c": '#include <api.h>\n',
        "inc/api.h": "",
        "build/scripts/clang_wrapper.py": "",
        "build/platform/clang/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG18 sbr:18)\nEND()\n",
        "build/platform/lld/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(LLD_ROOT sbr:19)\nEND()\n",
        "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
    }
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    return files


class BpfTest(unittest.TestCase):
    def test_plain_bpf_tracks_c_includes_and_passes_only_explicit_options(self):
        graph = lib.make(fixture("BPF(a.c code.bpf -DOPTION=1 -O2)\nRESOURCE(code.bpf key)\n"), "mod", *X86_64)
        node = lib.node_by_output(graph, "$(B)/mod/code.bpf")
        self.assertEqual(node["kv"], {"p": "BP", "pc": "light-green"})
        self.assertEqual(node["outputs"], ["$(B)/mod/code.bpf"])
        args = node["cmds"][0]["cmd_args"]
        self.assertEqual(args[:7], ["", "$(S)/build/scripts/clang_wrapper.py", "no",
                                   "$(B)/resources/CLANG18/bin/clang", "-I$(B)", "-I$(S)", "-I$(S)/inc"])
        self.assertEqual(args[-8:], ["-target", "bpf", "-c", "$(S)/mod/a.c", "-o", "$(B)/mod/code.bpf", "-DOPTION=1", "-O2"])
        self.assertNotIn("-DNOT_BPF", args)
        self.assertFalse(any(a.startswith("-std=") for a in args))
        self.assertCountEqual(node["inputs"], ["$(S)/build/scripts/clang_wrapper.py", "$(S)/mod/a.c", "$(S)/inc/api.h"])

    def test_static_bpf_links_a_native_object_before_noauto_source_dispatch(self):
        graph = lib.make(fixture("BPF_STATIC(sub/a.c nested/code.bpf -g)\n"), "mod", *X86_64)
        node = lib.node_by_output(graph, "$(B)/mod/nested/code.bpf")
        obj = "$(B)/mod/sub/a.c.bpf.o"
        self.assertEqual(node["outputs"], ["$(B)/mod/nested/code.bpf", obj])
        self.assertEqual(node["cmds"][1], {
            "cmd_args": ["$(B)/resources/LLD_ROOT/bin/ld.lld", "-r", "-b", "binary", "-m", "elf_x86_64", "-o", obj, "code.bpf"],
            "cwd": "$(B)/mod", "env": {"ARCADIA_ROOT_DISTBUILD": "$(S)"},
        })
        archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertIn(obj, archive["inputs"])
        self.assertIn(node["outputs"][0], archive["inputs"])
        self.assertIn(node["uid"], archive["deps"])

    def test_unused_plain_bpf_is_lazy_and_does_not_compile_its_binary(self):
        graph = lib.make(fixture("BPF(a.c unused.cpp)\n"), "mod", *X86_64)
        self.assertFalse(any(n["kv"].get("p") == "BP" for n in graph["graph"]))
        self.assertFalse(any("unused.cpp.o" in o for n in graph["graph"] for o in n["outputs"]))

    def test_generated_input_uses_its_existing_producer_and_include_metadata(self):
        graph = lib.make(fixture("COPY_FILE(a.c generated.c OUTPUT_INCLUDES inc/api.h)\nBPF_STATIC(generated.c code.bpf)\n"), "mod", *X86_64)
        copied = lib.node_by_output(graph, "$(B)/mod/generated.c")
        node = lib.node_by_output(graph, "$(B)/mod/code.bpf")
        self.assertIn(copied["uid"], node["deps"])
        self.assertIn("$(B)/mod/generated.c", node["inputs"])
        self.assertIn("$(S)/inc/api.h", node["inputs"])

    def test_platform_wrapper_and_binary_linker_follow_configuration(self):
        for target, windows in (("default-windows-x86_64", "yes"), ("default-darwin-x86_64", "no")):
            with self.subTest(target=target):
                graph = lib.make(fixture("BPF_STATIC(a.c code.bpf)\n"), "mod", "--target-platform", target)
                node = lib.node_by_output(graph, "$(B)/mod/code.bpf")
                self.assertEqual(node["cmds"][0]["cmd_args"][2], windows)
                self.assertEqual(node["cmds"][1]["cmd_args"][0], "$(B)/resources/LLD_ROOT/bin/ld")

    def test_sdk_platform_flags_are_passed_without_module_cflags(self):
        graph = lib.make(fixture("BPF_STATIC(a.c code.bpf)\n"), "mod", *X86_64, opensource=False)
        node = lib.node_by_output(graph, "$(B)/mod/code.bpf")
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("--sysroot=$(B)/resources/OS_SDK_ROOT", args)
        self.assertNotIn("-DNOT_BPF", args)

    def test_bpf_requires_input_and_output(self):
        for statement in ("BPF()", "BPF(a.c)", "BPF_STATIC(a.c)"):
            with self.subTest(statement=statement):
                process = lib.make_process(fixture(statement + "\n"), "mod", *X86_64)
                self.assertNotEqual(process.returncode, 0)
                self.assertIn("input and output", process.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
