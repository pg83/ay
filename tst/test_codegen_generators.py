import unittest

import lib


class GeneratorEdgeCasesTest(unittest.TestCase):
    def test_generator_macros_edge_paths(self):
        files = {
            "lib/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SPLIT_CODEGEN(tools/codegen factors_gen NOpt "
                "OUTPUT_INCLUDES lib/extra.h util/x.h)\n"
                "BASE_CODEGEN(tools/codegen other/base_gen)\n"
                "SRCS(cfg.h.in sub/data.rodata use.cpp)\n"
                "ARCHIVE(NAME data.inc a.txt a.txt)\n"
                "DECIMAL_MD5_LOWER_32_BITS(hash.h FUNCNAME get_hash a.txt)\n"
                "END()\n"
            ),
            "lib/factors_gen.in": "",
            "other/base_gen.in": "",
            "lib/extra.h": "",
            "util/x.h": "",
            "lib/a.txt": "",
            "lib/sub/data.rodata": "",
            "lib/cfg.h.in": '#define BT "@BUILD_TYPE@"\n',
            "lib/use.cpp": (
                '#include "factors_gen.h"\n#include "base_gen.h"\n#include "cfg.h"\n'
                '#include "data.inc"\n#include "hash.h"\n'
            ),
            "build/scripts/configure_file.py": "",
            "build/scripts/decimal_md5.py": "",
            "build/scripts/rodata2asm.py": "",
        }
        lib.tool_program(files, "tools/codegen", "codegen")
        lib.tool_program(files, "tools/archiver", "archiver")
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "lib", "--target-platform", "default-linux-x86_64")

        split = lib.node_by_output(graph, "$(B)/lib/factors_gen.h")
        self.assertEqual(split["cmds"][0]["cmd_args"], [
            "$(B)/tools/codegen/codegen", "$(S)/lib/factors_gen.in",
            "$(B)/lib/factors_gen.cpp", "$(B)/lib/factors_gen.h",
            "--cpp-parts", "20", "NOpt",
        ])

        base = lib.node_by_output(graph, "$(B)/lib/base_gen.h")
        self.assertEqual(base["cmds"][0]["cmd_args"], [
            "$(B)/tools/codegen/codegen", "$(S)/other/base_gen.in",
            "$(B)/lib/base_gen.cpp", "$(B)/lib/base_gen.h",
        ])
        self.assertEqual(base["inputs"], ["$(B)/tools/codegen/codegen", "$(S)/other/base_gen.in"])

        configure = lib.node_by_output(graph, "$(B)/lib/cfg.h")
        self.assertEqual(configure["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/configure_file.py",
            "$(S)/lib/cfg.h.in", "$(B)/lib/cfg.h", "BUILD_TYPE=DEBUG",
        ])

        archive = lib.node_by_output(graph, "$(B)/lib/data.inc")
        self.assertEqual(archive["cmds"][0]["cmd_args"][1:], [
            "-q", "-x", "$(S)/lib/a.txt:", "$(S)/lib/a.txt:", "-o", "$(B)/lib/data.inc",
        ])
        self.assertEqual(archive["inputs"], ["$(S)/lib/a.txt", "$(B)/tools/archiver/archiver"])

        digest = lib.node_by_output(graph, "$(B)/lib/hash.h")
        self.assertEqual(digest["kv"]["p"], "SV")
        self.assertFalse(any(
            output.startswith("$(B)/lib/hash.h.")
            for node in graph["graph"] for output in node["outputs"]
        ))

        rodata = lib.node_by_output(graph, "$(B)/lib/_/sub/data.rodata.o")
        self.assertEqual(rodata["outputs"], [
            "$(B)/lib/_/sub/data.rodata.asm", "$(B)/lib/_/sub/data.rodata.o",
        ])
        self.assertEqual(rodata["cmds"][0]["cmd_args"][1:], [
            "$(S)/build/scripts/rodata2asm.py", "--elf", "data",
            "$(S)/lib/sub/data.rodata", "$(B)/lib/_/sub/data.rodata.asm",
        ])

        use = lib.node_by_output(graph, "$(B)/lib/use.cpp.o")
        for path in (
            "$(B)/lib/factors_gen.h", "$(S)/lib/extra.h", "$(S)/util/x.h",
            "$(B)/lib/base_gen.h", "$(S)/other/base_gen.in",
            "$(B)/lib/cfg.h", "$(S)/lib/cfg.h.in",
            "$(B)/lib/data.inc", "$(B)/lib/hash.h", "$(S)/lib/a.txt",
        ):
            self.assertIn(path, use["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
