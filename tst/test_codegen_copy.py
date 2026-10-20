import unittest

import lib


LIBRARY_HEAD = "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
FS_TOOLS = "$(S)/build/scripts/fs_tools.py"


def copy_args(src, dst):
    return [FS_TOOLS, "copy", src, dst]


class CopyFileTest(unittest.TestCase):
    def test_default_copy_uses_text_input(self):
        for option in ("", " TEXT"):
            with self.subTest(option=option):
                graph = lib.make({
                    "mod/ya.make": (LIBRARY_HEAD + f"COPY_FILE(src.h dst.h{option})\n"
                                    "SRCS(use.cpp)\nEND()\n"),
                    "mod/src.h": '#include "dep.h"\n',
                    "mod/dep.h": "",
                    "mod/use.cpp": '#include "dst.h"\n',
                }, "mod", "--sandboxing")
                copied = lib.node_by_output(graph, "$(B)/mod/dst.h")
                self.assertEqual(copied["inputs"], ["$(S)/mod/src.h"])
                compiled = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
                self.assertNotIn("$(S)/mod/dep.h", compiled["inputs"])

    def test_copies_in_an_include_loop_share_their_inputs(self):
        # json_visitor.cpp keeps one input set per graph loop: ydb
        # yql/essentials/minikql/invoke_builtins/llvm16 copies
        # mkql_builtins_impl.h.txt and mkql_builtins_decimal.h.txt next to a
        # copy of mkql_builtins.h, their OUTPUT_INCLUDES form a cycle, and all
        # three CP nodes list the three sources.
        graph = lib.make({
            "mod/ya.make": (
                LIBRARY_HEAD + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                "COPY_FILE(TEXT a.h.txt ${BINDIR}/a.h OUTPUT_INCLUDES b.h)\n"
                "COPY_FILE(TEXT b.h.txt ${BINDIR}/b.h OUTPUT_INCLUDES a.h)\n"
                "COPY_FILE(TEXT c.h.txt ${BINDIR}/c.h OUTPUT_INCLUDES a.h)\n"
                "SRCS(use.cpp)\nEND()\n"
            ),
            "mod/a.h.txt": "", "mod/b.h.txt": "", "mod/c.h.txt": "",
            "mod/use.cpp": '#include "c.h"\n',
        }, "mod", "--sandboxing")
        for out, sources in (("a.h", ["a.h.txt", "b.h.txt"]), ("b.h", ["a.h.txt", "b.h.txt"]), ("c.h", ["c.h.txt"])):
            inputs = lib.node_by_output(graph, f"$(B)/mod/{out}")["inputs"]
            self.assertEqual(sorted(i for i in inputs if i.endswith(".txt")), [f"$(S)/mod/{s}" for s in sources], out)

    def test_branching_copy_cycle_keeps_all_member_sources(self):
        for entry in ("a", "b", "c"):
            with self.subTest(entry=entry):
                graph = lib.make({
                    "mod/ya.make": (
                        LIBRARY_HEAD + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                        "COPY_FILE(TEXT a.txt a.h OUTPUT_INCLUDES b.h)\n"
                        "COPY_FILE_WITH_CONTEXT(src_b.h b.h OUTPUT_INCLUDES a.h c.h)\n"
                        "COPY_FILE(TEXT c.txt c.h OUTPUT_INCLUDES a.h)\n"
                        "SRCS(use.cpp)\nEND()\n"
                    ),
                    "mod/a.txt": "", "mod/src_b.h": "", "mod/c.txt": "",
                    "mod/use.cpp": f'#include "{entry}.h"\n',
                }, "mod")
                for out in ("a", "b", "c"):
                    inputs = lib.node_by_output(graph, f"$(B)/mod/{out}.h")["inputs"]
                    for source in ("a.txt", "src_b.h", "c.txt"):
                        self.assertIn(f"$(S)/mod/{source}", inputs, out)

    def test_copy_cycle_through_a_source_header_shares_inputs(self):
        graph = lib.make({
            "mod/ya.make": (
                LIBRARY_HEAD + "ADDINCL(${ARCADIA_BUILD_ROOT}/mod)\n"
                "COPY_FILE(TEXT a.h.txt ${BINDIR}/a.h OUTPUT_INCLUDES bridge.h)\n"
                "COPY_FILE(TEXT b.h.txt ${BINDIR}/b.h OUTPUT_INCLUDES a.h)\n"
                "COPY_FILE(TEXT tail.h.txt ${BINDIR}/tail.h OUTPUT_INCLUDES a.h)\n"
                "SRCS(use.cpp)\nEND()\n"
            ),
            "mod/a.h.txt": "", "mod/b.h.txt": "", "mod/tail.h.txt": "",
            "mod/bridge.h": '#include "b.h"\n',
            "mod/use.cpp": '#include "tail.h"\n',
        }, "mod", "--sandboxing")
        for out, sources in (
            ("a.h", {"a.h.txt", "b.h.txt"}),
            ("b.h", {"a.h.txt", "b.h.txt"}),
            ("tail.h", {"tail.h.txt"}),
        ):
            inputs = lib.node_by_output(graph, f"$(B)/mod/{out}")["inputs"]
            self.assertEqual(
                {i for i in inputs if i.endswith(".txt")},
                {f"$(S)/mod/{s}" for s in sources}, out,
            )

    def test_context_output_includes_chained_and_object_copies(self):
        graph = lib.make({
            "mod/ya.make": (
                LIBRARY_HEAD
                + "COPY_FILE_WITH_CONTEXT(ctx.h ctx_copy.h)\n"
                + "COPY_FILE(data.txt plain.h OUTPUT_INCLUDES extra.h)\n"
                + "COPY_FILE(${BINDIR}/plain.h second.h)\n"
                + "COPY_FILE(blob.bin blob.o)\n"
                + "SRCS(use.cpp)\n"
                + "END()\n"
            ),
            "mod/ctx.h": '#include "dep.h"\n',
            "mod/dep.h": "",
            "mod/extra.h": "",
            "mod/data.txt": "",
            "mod/blob.bin": "",
            "mod/use.cpp": (
                '#include "ctx_copy.h"\n#include "plain.h"\n#include "second.h"\n'
            ),
        }, "mod")

        context = lib.node_by_output(graph, "$(B)/mod/ctx_copy.h")
        self.assertEqual(context["kv"], {"p": "CP", "pc": "light-cyan"})
        self.assertEqual(
            context["cmds"][0]["cmd_args"][1:],
            copy_args("$(S)/mod/ctx.h", "$(B)/mod/ctx_copy.h"),
        )
        self.assertEqual(context["inputs"], ["$(S)/mod/ctx.h", "$(S)/mod/dep.h"])

        plain = lib.node_by_output(graph, "$(B)/mod/plain.h")
        self.assertEqual(plain["inputs"], ["$(S)/mod/data.txt"])

        second = lib.node_by_output(graph, "$(B)/mod/second.h")
        self.assertEqual(
            second["cmds"][0]["cmd_args"][1:],
            copy_args("$(B)/mod/plain.h", "$(B)/mod/second.h"),
        )
        self.assertEqual(second["inputs"], ["$(B)/mod/plain.h", "$(S)/mod/extra.h"])
        self.assertEqual(second["deps"], [plain["uid"]])

        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertEqual(sorted(use["inputs"]), sorted([
            "$(S)/mod/use.cpp",
            "$(B)/mod/ctx_copy.h", "$(S)/mod/ctx.h", "$(S)/mod/dep.h",
            "$(B)/mod/plain.h", "$(S)/mod/extra.h",
            "$(B)/mod/second.h",
        ]))

        blob = lib.node_by_output(graph, "$(B)/mod/blob.o")
        library = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertEqual(library["inputs"][:2], ["$(B)/mod/blob.o", "$(B)/mod/use.cpp.o"])
        self.assertIn(blob["uid"], library["deps"])

    def test_second_copy_to_same_destination_keeps_first_producer(self):
        graph = lib.make({
            "mod/ya.make": (
                LIBRARY_HEAD
                + "COPY_FILE(data.txt plain.h)\n"
                + "COPY_FILE(other.txt plain.h)\n"
                + "SRCS(use.cpp)\nEND()\n"
            ),
            "mod/data.txt": "",
            "mod/other.txt": "",
            "mod/use.cpp": '#include "plain.h"\n',
        }, "mod")
        copies = [
            node for node in graph["graph"]
            if "$(B)/mod/plain.h" in node["outputs"]
        ]
        self.assertEqual(len(copies), 1)
        self.assertEqual(
            copies[0]["cmds"][0]["cmd_args"][1:],
            copy_args("$(S)/mod/data.txt", "$(B)/mod/plain.h"),
        )

    def test_auto_copy_feeds_codegen_source(self):
        files = {
            "mod/ya.make": (
                LIBRARY_HEAD + "COPY_FILE(AUTO gram.src gram.y)\nSRCS(own.y)\nEND()\n"
            ),
            "mod/gram.src": "%%\n",
            "mod/own.y": "%%\n",
            "build/scripts/preprocess.py": "",
            "build/induced/by_bison/ya.make": "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nEND()\n",
        }
        for skeleton in (
            "m4sugar/foreach.m4", "m4sugar/m4sugar.m4", "skeletons/bison.m4",
            "skeletons/c++-skel.m4", "skeletons/c++.m4", "skeletons/c-like.m4",
            "skeletons/c-skel.m4", "skeletons/c.m4", "skeletons/glr.cc",
            "skeletons/lalr1.cc", "skeletons/location.cc", "skeletons/stack.hh",
            "skeletons/variant.hh", "skeletons/yacc.c",
        ):
            files[f"contrib/tools/bison/data/{skeleton}"] = ""
        lib.tool_program(files, "contrib/tools/bison", "bison")
        lib.tool_program(files, "contrib/tools/m4", "m4")
        graph = lib.make(files, "mod")

        copy = lib.node_by_output(graph, "$(B)/mod/gram.y")
        bison = lib.node_by_output(graph, "$(B)/mod/gram.y.cpp")
        self.assertEqual(bison["cmds"][0]["cmd_args"][-1], "$(B)/mod/gram.y")
        self.assertEqual(bison["inputs"][2], "$(B)/mod/gram.y")
        self.assertIn(copy["uid"], bison["deps"])
        compile_node = lib.node_by_output(graph, "$(B)/mod/gram.y.cpp.o")
        self.assertNotIn("$(S)/mod/gram.src", compile_node["inputs"])
        own = lib.node_by_output(graph, "$(B)/mod/own.y.cpp")
        self.assertEqual(own["cmds"][0]["cmd_args"][-1], "$(S)/mod/own.y")


class LdPluginTest(unittest.TestCase):
    def test_ld_plugin_is_copied_and_passed_to_linker(self):
        graph = lib.make({
            "prog/ya.make": (
                "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(main.cpp)\nLD_PLUGIN(plugin.py)\nEND()\n"
            ),
            "prog/main.cpp": "int main(){return 0;}\n",
            "prog/plugin.py": "",
        }, "prog")
        plugin = lib.node_by_output(graph, "$(B)/prog/plugin.py.pyplugin")
        self.assertEqual(plugin["kv"], {"p": "CP", "pc": "light-cyan"})
        self.assertEqual(
            plugin["cmds"][0]["cmd_args"][1:],
            copy_args("$(S)/prog/plugin.py", "$(B)/prog/plugin.py.pyplugin"),
        )
        self.assertEqual(plugin["inputs"], ["$(S)/prog/plugin.py"])
        link = lib.node_by_output(graph, "$(B)/prog/prog")
        args = link["cmds"][2]["cmd_args"]
        start = args.index("--start-plugins")
        self.assertEqual(args[start:start + 3], [
            "--start-plugins", "$(B)/prog/plugin.py.pyplugin", "--end-plugins",
        ])
        self.assertIn(plugin["uid"], link["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
