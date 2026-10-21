import unittest

import lib
from test_go_tests import go_world
from test_ros import ros_files, CPP_DEPS


def files(shared=False):
    sources = go_world()
    sources.update(ros_files())
    dictionary = CPP_DEPS[0]
    sources[dictionary + "/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}GEN_PCH_FILE({dictionary}/private.h ${{BINDIR}}/private.h.pch)\nSRCS(local.cpp registrator.h)\nEND()\n"
    sources[dictionary + "/local.cpp"] = "int local;\n"
    sources[dictionary + "/private.h"] = "#pragma once\n"
    sources[dictionary + "/registrator.h"] = f'#if 0\n#include "{dictionary}/private.h.pch"\n#endif\n'
    sources.update({
        "msgs/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ONLY_TAGS(CPP_ROS GO_ROS)\nROS_SRCS(msg/Plain.msg)\nEND()\n",
        "msgs/msg/Plain.msg": "int32 value\n",
        "dependent/ya.make": f"ROS_LIBRARY(child)\n{lib.BARE_MODULE}ONLY_TAGS(CPP_ROS GO_ROS)\nPEERDIR(msgs)\nROS_SRCS(msg/Child.msg)\nEND()\n",
        "dependent/msg/Child.msg": "robot/Plain value\n",
        "app/ya.make": f"GO_LIBRARY()\n{lib.BARE_MODULE}PEERDIR(dependent)\nSRCS(app.go)\nEND()\n",
        "app/app.go": "package app\n",
        "build/scripts/writer.py": "",
        "native/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}SRCS(main.cpp)\nPEERDIR(msgs)\nEND()\n",
        "native/main.cpp": "int main() {}\n",
    })
    lib.tool_program(sources, "sdg/tier0/tools/make_pch_wrapper", "make_pch_wrapper")
    if shared:
        for module, stem, partial in (("msgs", "Plain", "robot_msgs"), ("dependent", "Child", "child_msgs")):
            sources[module + "/ya.make"] = sources[module + "/ya.make"].replace("ROS_SRCS(", f"ROS_DIR(shared)\nSRCDIR(shared)\nROS_PARTIAL({partial})\nROS_SRCS(")
            sources[f"shared/msg/{stem}.msg"] = sources.pop(f"{module}/msg/{stem}.msg")
    return sources


class GoRosPrecompiledHeaderTest(unittest.TestCase):
    def test_go_ros_does_not_inherit_cpp_variant_precompiled_headers(self):
        for shared, targets in ((False, ("native", "app")), (False, ("app", "native")), (False, ("msgs", "dependent", "native", "app")), (True, ("native", "app")), (True, ("app", "native"))):
            with self.subTest(shared=shared, targets=targets):
                graph = lib.make(files(shared), *targets)
                native = lib.node_by_output(graph, "$(B)/native/native")
                pch = "$(B)/" + CPP_DEPS[0] + "/private.h.pch"
                self.assertIn(pch, native["inputs"])
                self.assertIn(lib.node_by_output(graph, pch)["uid"], native["deps"])
                for output in ("$(B)/msgs/robot.a", "$(B)/dependent/child.a", "$(B)/app/app.a"):
                    node = lib.node_by_output(graph, output)
                    self.assertNotIn("$(B)/" + CPP_DEPS[0] + "/private.h.pch", node["inputs"])
                generated = "$(B)/shared/Plain.go" if shared else "$(B)/msgs/Plain.go"
                self.assertIn(generated, lib.node_by_output(graph, "$(B)/msgs/robot.a")["inputs"])

    def test_go_consumer_preserves_native_peer_precompiled_headers(self):
        sources = files()
        sources["app/ya.make"] = sources["app/ya.make"].replace("PEERDIR(dependent)", f"PEERDIR({CPP_DEPS[0]})")
        graph = lib.make(sources, "app")
        consumer = lib.node_by_output(graph, "$(B)/app/app.a")
        pch = "$(B)/" + CPP_DEPS[0] + "/private.h.pch"
        producer = lib.node_by_output(graph, pch)
        self.assertIn(pch, consumer["inputs"])
        self.assertIn(producer["uid"], consumer["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
