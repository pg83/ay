import unittest

import lib


def tool_program(files, path, name):
    files[f"{path}/ya.make"] = (
        f"PROGRAM({name})\n"
        "NO_LIBC()\n"
        "NO_RUNTIME()\n"
        "NO_UTIL()\n"
        "SRCS(main.cpp)\n"
        "END()\n"
    )
    files[f"{path}/main.cpp"] = "int main(){return 0;}\n"


class EmitArchivesTest(unittest.TestCase):
    def test_plain_archive_propagates_source_members(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(use.cpp)\n"
                "ARCHIVE(NAME data.inc payload.lst)\n"
                "END()\n"
            ),
            "mod/payload.lst": "row\n",
            "mod/use.cpp": '#include "data.inc"\n',
        }
        tool_program(files, "tools/archiver", "archiver")
        graph = lib.make(files, "mod")
        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn("$(S)/mod/payload.lst", use["inputs"])

    def test_archive_by_keys_top_level(self):
        files = {
            "mod/ya.make": (
                "LIBRARY()\n"
                "NO_LIBC()\n"
                "NO_RUNTIME()\n"
                "NO_UTIL()\n"
                "SRCS(use.cpp)\n"
                "ARCHIVE_BY_KEYS(\n"
                "  NAME data.inc\n"
                "  KEYS /k1:/k2\n"
                "  a.txt\n"
                "  sub/b.txt\n"
                ")\n"
                "END()\n"
            ),
            "mod/a.txt": "alpha\n",
            "mod/sub/b.txt": "beta\n",
            "mod/use.cpp": '#include "data.inc"\n',
        }
        tool_program(files, "tools/archiver", "archiver")
        graph = lib.make(files, "mod")
        archive = lib.node_by_output(graph, "$(B)/mod/data.inc")
        self.assertEqual(archive["kv"]["p"], "AR")
        self.assertEqual(archive["kv"]["pc"], "light-red")
        command = " ".join(archive["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/mod/a.txt $(S)/mod/sub/b.txt", command)
        self.assertIn("-k /k1:/k2", command)
        self.assertIn("-o $(B)/mod/data.inc", command)
        self.assertNotIn("$(S)/mod/a.txt:", command)
        self.assertIn("$(S)/mod/a.txt", archive["inputs"])
        self.assertIn("$(S)/mod/sub/b.txt", archive["inputs"])

        use = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn("-I$(B)/mod", use["cmds"][0]["cmd_args"])
        self.assertIn("$(S)/mod/a.txt", use["inputs"])
        self.assertIn("$(S)/mod/sub/b.txt", use["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
