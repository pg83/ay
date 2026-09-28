import unittest

import lib


def library(sources):
    return (
        "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
        f"SRCS({sources})\nEND()\n"
    )


def closure(graph, source):
    return {
        path[len("$(S)/m/"):]
        for path in lib.node_by_output(graph, f"$(B)/m/{source}.o")["inputs"]
    }


class IncludeCycleTest(unittest.TestCase):
    def test_strongly_connected_headers_share_one_closure(self):
        files = {
            "m/ya.make": library("a.cpp b.cpp c.cpp s.cpp"),
            # x -> y -> z -> x is one component; d is reached from two
            # members and e only from the deepest one.
            "m/a.cpp": '#include "x.h"\n',
            "m/x.h": '#include "d.h"\n#include "y.h"\n',
            "m/y.h": '#include "z.h"\n',
            "m/z.h": '#include "x.h"\n#include "d.h"\n#include "e.h"\n',
            "m/d.h": "",
            "m/e.h": "",
            "m/b.cpp": '#include "z.h"\n',
            # A second, independent two-node cycle reuses the pooled
            # Tarjan state.
            "m/c.cpp": '#include "p.h"\n',
            "m/p.h": '#include "q.h"\n',
            "m/q.h": '#include "p.h"\n#include "r.h"\n',
            "m/r.h": '#include "q.h"\n',
            "m/s.cpp": '#include "self.h"\n',
            "m/self.h": '#include "self.h"\n#include "d.h"\n',
        }
        graph = lib.make(files, "m")
        component = {"x.h", "y.h", "z.h", "d.h", "e.h"}
        self.assertEqual({"a.cpp"} | component, closure(graph, "a.cpp"))
        self.assertEqual({"b.cpp"} | component, closure(graph, "b.cpp"))
        self.assertEqual({"c.cpp", "p.h", "q.h", "r.h"}, closure(graph, "c.cpp"))
        self.assertEqual({"s.cpp", "self.h", "d.h"}, closure(graph, "s.cpp"))

    def test_cycle_entered_from_many_sources(self):
        headers = [f"h{i}.h" for i in range(6)]
        files = {"m/ya.make": library(" ".join(f"s{i}.cpp" for i in range(6)))}
        for i, header in enumerate(headers):
            following = headers[(i + 1) % len(headers)]
            files[f"m/{header}"] = f'#include "{following}"\n#include "leaf{i % 2}.h"\n'
            files[f"m/s{i}.cpp"] = f'#include "{header}"\n'
        files["m/leaf0.h"] = ""
        files["m/leaf1.h"] = ""
        graph = lib.make(files, "m")
        for i in range(6):
            self.assertEqual(
                {f"s{i}.cpp", "leaf0.h", "leaf1.h", *headers},
                closure(graph, f"s{i}.cpp"),
            )


if __name__ == "__main__":
    unittest.main(verbosity=2)
