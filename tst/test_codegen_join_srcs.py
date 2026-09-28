import unittest

import lib


class JoinSrcsClosureTest(unittest.TestCase):
    def graph(self, platform):
        return lib.make({
            "joinmod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCDIR(shared)\n"
                "JOIN_SRCS(all_my.cpp src1.cpp src2.cpp far.cpp)\n"
                "END()\n"
            ),
            "joinmod/src1.cpp": '#include "common.h"\n#include "src2.cpp"\n',
            "joinmod/src2.cpp": '#include "common.h"\n',
            "joinmod/common.h": "",
            "shared/far.cpp": '#include "far.h"\n',
            "shared/far.h": "",
            "build/scripts/gen_join_srcs.py": "",
        }, "joinmod", "--target-platform", platform)

    def test_include_closure_is_deduplicated_and_follows_srcdir(self):
        for platform in ("default-linux-aarch64", "default-linux-x86_64"):
            with self.subTest(platform=platform):
                graph = self.graph(platform)
                join = lib.node_by_output(graph, "$(B)/joinmod/all_my.cpp")
                self.assertEqual(join["inputs"][0], "$(S)/build/scripts/gen_join_srcs.py")
                self.assertEqual(join["inputs"][-2:], [
                    "$(S)/joinmod/common.h", "$(S)/shared/far.h",
                ])
                compile_node = lib.node_by_output(graph, "$(B)/joinmod/all_my.cpp.o")
                self.assertEqual(compile_node["inputs"][0], "$(B)/joinmod/all_my.cpp")
                for path in ("$(S)/joinmod/common.h", "$(S)/joinmod/src1.cpp",
                             "$(S)/joinmod/src2.cpp", "$(S)/shared/far.h"):
                    self.assertEqual(compile_node["inputs"].count(path), 1)


if __name__ == "__main__":
    unittest.main(verbosity=2)
