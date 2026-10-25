import unittest

import lib


class EmitDecimalMD5Test(unittest.TestCase):
    def test_source_option_keeps_text_boundary_after_another_consumer_parses_it(self):
        files = {
            "md5_owner/ya.make": (
                "LIBRARY()\n" + lib.BARE_MODULE + "SRCS(input.cpp)\nEND()\n"
            ),
            "md5_owner/input.cpp": '#include "leaf.h"\n',
            "md5_owner/leaf.h": "// parsed dependency\n",
            "md5_text/ya.make": (
                "LIBRARY()\n" + lib.BARE_MODULE
                + "DECIMAL_MD5_LOWER_32_BITS("
                "hash.cpp FUNCNAME hash ${ARCADIA_ROOT}/md5_owner/input.cpp)\n"
                "END()\n"
            ),
        }
        for targets in (("md5_owner", "md5_text"), ("md5_text", "md5_owner")):
            with self.subTest(targets=targets):
                graph = lib.make(files, targets[-1], *targets[:-1])
                compiler = lib.node_by_output(graph, "$(B)/md5_owner/input.cpp.o")
                self.assertIn("$(S)/md5_owner/leaf.h", compiler["inputs"])
                producer = lib.node_by_output(graph, "$(B)/md5_text/hash.cpp")
                self.assertEqual(set(producer["inputs"]), {
                    "$(S)/build/scripts/decimal_md5.py",
                    "$(S)/md5_owner/input.cpp",
                })
                self.assertNotIn(compiler["uid"], producer["deps"])

    def test_generated_source_enters_archive(self):
        graph = lib.make({
            "mod/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SET(HASH_INPUTS data.txt helper.hpp)\n"
                "DECIMAL_MD5_LOWER_32_BITS("
                "hash.auto.cpp FUNCNAME get_hash ${HASH_INPUTS})\n"
                "SRCS(main.cpp)\n"
                "END()\n"
            ),
            "mod/data.txt": "payload\n",
            "mod/helper.hpp": "// helper\n",
            "mod/main.cpp": "int main(){return 0;}\n",
            "build/scripts/decimal_md5.py": "\n",
        }, "mod")
        producer = lib.node_by_output(graph, "$(B)/mod/hash.auto.cpp")
        self.assertEqual(producer["kv"]["p"], "SV")
        self.assertEqual(producer["kv"]["pc"], "yellow")
        self.assertTrue(producer["kv"]["show_out"])
        command = " ".join(producer["cmds"][0]["cmd_args"])
        for expected in (
            "build/scripts/decimal_md5.py",
            "--fixed-output=",
            "--func-name=get_hash",
            "--lower-bits 32",
            "--source-root=$(S)",
            "$(S)/mod/data.txt",
            "$(S)/mod/helper.hpp",
        ):
            self.assertIn(expected, command)
        for expected in (
            "$(S)/mod/data.txt",
            "$(S)/mod/helper.hpp",
            "$(S)/build/scripts/decimal_md5.py",
        ):
            self.assertIn(expected, producer["inputs"])

        compile_node = lib.node_by_output(graph, "$(B)/mod/hash.auto.cpp.o")
        self.assertEqual(compile_node["kv"]["p"], "CC")
        for expected in ('$(B)/mod/hash.auto.cpp',):
            self.assertIn(expected, compile_node["inputs"])
        self.assertNotIn('$(S)/mod/data.txt', compile_node['inputs'])
        self.assertNotIn('$(S)/mod/helper.hpp', compile_node['inputs'])
        self.assertNotIn('$(S)/build/scripts/decimal_md5.py', compile_node['inputs'])
        self.assertIn(producer["uid"], compile_node["deps"])

        archive = lib.node_by_output(graph, "$(B)/mod/libmod.a")
        self.assertIn("$(B)/mod/hash.auto.cpp.o", archive["inputs"])
        self.assertIn(
            "$(B)/mod/hash.auto.cpp.o",
            archive["cmds"][0]["cmd_args"],
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
