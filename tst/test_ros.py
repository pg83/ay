import unittest

import lib


GENERATOR = "sdg/sdc/ros/misc/ros_msg_generator/yamakes/generator"
CPP_DEPS = (
    "sdg/sdc/ros/misc/message_serialization_dictionary",
    "sdg/sdc/bros/common",
    "sdg/sdc/bros/serialization/yamakes/libroscpp_serialization",
    "sdg/sdc/bros/traits/yamakes/roscpp_traits",
)


def ros_files():
    files = {f"{p}/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}\nEND()\n" for p in CPP_DEPS}
    for header in (
        "sdg/sdc/core/rtb/inplace_vector.h", "sdg/sdc/core/rtb/optional.h",
        "sdg/sdc/core/rtb/optional_io.h", "sdg/sdc/core/rtb/inplace_str.h",
        "sdg/sdc/ros/misc/message_serialization_dictionary/registrator.h",
        "sdg/sdc/bros/serialization/include/ros/serialization.h",
        "sdg/sdc/bros/serialization/include/ros/serialization_streams.h",
        "sdg/sdc/bros/serialization/include/ros/serializer.h",
        "sdg/sdc/bros/traits/include/ros/builtin_message_traits.h",
        "sdg/sdc/bros/traits/include/ros/message_operations.h",
        "sdg/sdc/bros/traits/include/ros/service_traits.h",
        "sdg/sdc/bros/time/include/ros/duration.h", "sdg/sdc/bros/time/include/ros/time.h",
        "sdg/sdc/ros/proto/rosmsg/common_msgs/Duration_ros_converter.h",
        "sdg/sdc/ros/proto/rosmsg/common_msgs/Duration_zero_copy.h",
        "sdg/sdc/ros/proto/rosmsg/common_msgs/Time_ros_converter.h",
        "sdg/sdc/ros/proto/rosmsg/common_msgs/Time_zero_copy.h",
    ):
        files[header] = ""
    lib.tool_program(files, GENERATOR, "generator")
    return files


class RosTests(unittest.TestCase):
    def test_python_binding_keeps_global_ros_includes_before_python_and_cpp_peer(self):
        from test_python_srcs import STUB_LIBRARY, python_base

        files = ros_files()
        files.update(python_base())
        for peer in (
            "library/python/runtime_py3",
            "contrib/python/numpy/py3",
            "sdg/sdc/ros/misc/ros_msg_generator/yamakes/py_cpp_bindings_support_lib",
            "sdg/sdc/ros/misc/python_utils/yamakes/ros_converters",
        ):
            files[f"{peer}/ya.make"] = STUB_LIBRARY
        lib.tool_program(
            files, "sdg/tier0/tools/generate_export_py_cpp_bindings",
            "generate_export_py_cpp_bindings",
        )
        for header in (
            "sdg/sdc/ros/misc/python_utils/ros_converters.h",
            "sdg/sdc/ros/misc/python_utils/stl_converters.h",
            "sdg/sdc/ros/misc/python_utils/numpy_wrapper.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/helpers.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export_arrays.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export_enum.h",
        ):
            files[header] = ""
        files.update({
            "msgs/ya.make": (
                "ROS_LIBRARY(robot)\n" + lib.BARE_MODULE
                + "ONLY_TAGS(CPP_ROS PY3_CPP_ROS)\n"
                "ROS_DIR(generated/robot)\nROS_SRCS(Topic.msg)\nEND()\n"
            ),
            "msgs/Topic.msg": "int32 value\n",
        })

        graph = lib.make(files, "msgs", "--sandboxing")
        cpp = lib.node_by_output(graph, "$(B)/generated/robot/Topic.cc")
        python = lib.node_by_output(graph, "$(B)/generated/robot/Topic.py.cc")
        compiled = lib.node_by_output(graph, "$(B)/msgs/__/generated/robot/Topic.py.cc.o")
        self.assertNotEqual(cpp["uid"], python["uid"])
        self.assertIn(python["uid"], compiled["deps"])
        self.assertIn("$(B)/generated/robot/Topic.py.cc", compiled["inputs"])
        includes = [arg for arg in compiled["cmds"][0]["cmd_args"] if arg.startswith("-I")]
        self.assertEqual(includes[:7], [
            "-I$(B)", "-I$(S)",
            "-I$(B)/generated/robot", "-I$(B)/generated",
            "-I$(S)/contrib/libs/python/Include",
            "-I$(B)/generated/robot", "-I$(B)/generated",
        ])
        archive = next(node for node in graph["graph"] if
                       node["kv"]["p"] == "AR" and compiled["outputs"][0] in node["inputs"])
        self.assertIn(compiled["uid"], archive["deps"])

    def test_owned_and_propagated_include_directories_remain_separate(self):
        files = ros_files()
        files.update({
            "dep/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}\nROS_DIR(shared)\nROS_SRCS(Child.msg)\nEND()\n",
            "dep/Child.msg": "int32 value\n",
            "msgs/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}\nONLY_TAGS(CPP_ROS)\nROS_DIR(shared)\nPEERDIR(dep)\nROS_SRCS(Parent.msg)\nEND()\n",
            "msgs/Parent.msg": "int32 value\n",
        })
        graph = lib.make(files, "msgs")
        generated = lib.node_by_output(graph, "$(B)/shared/Parent.h")
        args = generated["cmds"][0]["cmd_args"]
        self.assertEqual(args.count("-Irobot:shared"), 2)
        self.assertEqual(args.count("-Irobot:shared/msg"), 2)
        self.assertLess(args.index("-Irobot:msgs"), args.index("-Irobot:dep"))

    def test_go_peer_selects_go_ros_and_emits_empty_package(self):
        from test_link_go_modules import fixture
        files = fixture(sbom=False)
        files.update(ros_files())
        files.update({
            "msgs/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}\nROS_DIR(msgs)\nROS_SRCS(msg/Plain.msg srv/Query.srv)\nEND()\n",
            "msgs/msg/Plain.msg": "int32 value\n",
            "msgs/srv/Query.srv": "int32 value\n---\nint32 result\n",
            "app/ya.make": f"GO_PROGRAM()\n{lib.BARE_MODULE}\nPEERDIR(msgs)\nEND()\n",
            "build/scripts/writer.py": "",
        })
        graph = lib.make(files, "app")
        generated = lib.node_by_output(graph, "$(B)/msgs/Plain.go")
        self.assertEqual(generated["cmds"][0]["cmd_args"][2], "golang")
        empty = lib.node_by_output(graph, "$(B)/msgs/empty.go")
        self.assertEqual(empty["kv"]["p"], "RS")
        package = lib.node_by_output(graph, "$(B)/msgs/robot.a")
        self.assertIn(generated["uid"], package["deps"])
        self.assertIn(empty["uid"], package["deps"])
        self.assertFalse(any(o.endswith("Query.go") or o.endswith("Plain.cc") for n in graph["graph"] for o in n["outputs"]))

    def test_go_peer_cpp_source_consumes_cpp_ros_headers_too(self):
        from test_link_go_modules import fixture
        files = fixture(sbom=False)
        files.update(ros_files())
        files.update({
            "msgs/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ROS_DIR(msgs)\n"
                            "ROS_SRCS(ZERO_COPY msg/Fast.msg)\nEND()\n",
            "msgs/msg/Fast.msg": "int32 value\n",
            "bridge/ya.make": f"GO_LIBRARY()\n{lib.BARE_MODULE}PEERDIR(msgs)\n"
                              "SRCS(compat.cpp)\nUSE_CXX()\nEND()\n",
            "bridge/compat.cpp": "#include <msgs/Fast_zero_copy.h>\nint compatible;\n",
            "app/ya.make": f"GO_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(bridge)\nEND()\n",
            "build/scripts/writer.py": "",
        })
        graph = lib.make(files, "app", "--sandboxing")
        go_generator = lib.node_by_output(graph, "$(B)/msgs/Fast.go")
        cpp_generator = lib.node_by_output(graph, "$(B)/msgs/Fast_zero_copy.h")
        compiled = lib.node_by_output(graph, "$(B)/bridge/compat.cpp.o")
        go_archive = lib.node_by_output(graph, "$(B)/msgs/robot.a")
        self.assertEqual(go_generator["cmds"][0]["cmd_args"][2], "golang")
        self.assertEqual(cpp_generator["cmds"][0]["cmd_args"][2], "cpp")
        self.assertIn(go_generator["uid"], go_archive["deps"])
        self.assertIn(cpp_generator["uid"], compiled["deps"])
        self.assertIn("$(B)/msgs/Fast.cc", compiled["inputs"])
        self.assertIn("$(B)/msgs/Fast_zero_copy.h", compiled["inputs"])

    def test_python_ros_uses_binding_and_cpp_self_peers(self):
        from test_proto_py_variant import py_proto_files
        files = py_proto_files()
        files.update(ros_files())
        for peer in (
            "contrib/python/numpy/py3",
            "sdg/sdc/ros/misc/ros_msg_generator/yamakes/py_cpp_bindings_support_lib",
            "sdg/sdc/ros/misc/python_utils/yamakes/ros_converters",
            "sdg/sdc/third_party/ros/genpy/yamakes/pylib",
            "library/python/runtime_py3",
        ):
            files[f"{peer}/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}\nEND()\n"
        for header in (
            "sdg/sdc/ros/misc/python_utils/numpy_wrapper.h",
            "sdg/sdc/ros/misc/python_utils/ros_converters.h",
            "sdg/sdc/ros/misc/python_utils/stl_converters.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/helpers.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export_arrays.h",
            "sdg/sdc/ros/misc/ros_msg_generator/py_cpp/bindings_support_lib/export_enum.h",
        ):
            files[header] = ""
        for tool in ("generate_proxy_import_file", "generate_export_py_cpp_bindings"):
            lib.tool_program(files, "sdg/tier0/tools/" + tool, tool)
        files.update({
            "msgs/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}\nROS_DIR(msgs)\nROS_PARTIAL(part)\nROS_SRCS(msg/Plain.msg PARTIAL extra)\nEND()\n",
            "msgs/msg/Plain.msg": "int32 value\n",
            "py/ya.make": f"PY3_PROGRAM()\n{lib.BARE_MODULE}\nPEERDIR(msgs)\nEND()\n",
        })
        graph = lib.make(files, "py")
        binding = lib.node_by_output(graph, "$(B)/msgs/Plain.py.h")
        self.assertEqual(binding["cmds"][0]["cmd_args"][2], "python_cpp_bindings")
        lib.node_by_output(graph, "$(B)/msgs/Plain.h")
        export = lib.node_by_output(graph, "$(B)/msgs/part_py_cpp_bindings.cpp")
        self.assertIn("--partial-name=part", export["cmds"][0]["cmd_args"])
        archive = lib.node_by_output(graph, "$(B)/msgs/libpy3_cpp_robot.a")
        objects = [arg for arg in archive["cmds"][0]["cmd_args"] if arg.endswith(".o")]
        self.assertEqual(objects, ["$(B)/msgs/Plain.py.cc.o", "$(B)/msgs/part_py_cpp_bindings.cpp.o"])
        proxy = lib.node_by_output(graph, "$(B)/msgs/msg/__init__.py")
        self.assertEqual(proxy["cmds"][0]["cmd_args"][1:], ["--from-import=..cpp_msg", "--from-import=extra.msg"])

    def test_cpp_messages_zero_copy_and_service_outputs(self):
        files = ros_files()
        files.update({
            "msgs/ya.make": f"""ROS_LIBRARY(robot)
{lib.BARE_MODULE}
ONLY_TAGS(CPP_ROS)
ROS_DIR(generated/robot)
ROS_SRCS(msg/Plain.msg ZERO_COPY msg/Fast.msg srv/Query.srv)
END()
""",
            "msgs/msg/Plain.msg": "int32 value\n",
            "msgs/msg/Fast.msg": "int32 value\n",
            "msgs/srv/Query.srv": "int32 request\n---\nbool result\n",
        })
        graph = lib.make(files, "msgs")
        plain = lib.node_by_output(graph, "$(B)/generated/robot/Plain.h")
        self.assertEqual(plain["kv"]["p"], "RS")
        self.assertEqual(plain["outputs"], ["$(B)/generated/robot/Plain.cc", "$(B)/generated/robot/Plain.h"])
        self.assertEqual(plain["cmds"][0]["cwd"], "$(S)")
        self.assertEqual(plain["cmds"][0]["cmd_args"][1:], [
            "--language", "cpp", "--repository-root", "$(S)",
            "--third-party-root", "$(S)/sdg/sdc/third_party_root/ros", "--package", "robot",
            "-Irobot:msgs", "-Irobot:generated/robot", "-Irobot:generated/robot/msg", "-Irobot:generated/robot/srv",
            "$(S)/msgs/msg/Plain.msg", "--output-dir", "$(B)/generated/robot",
        ])
        fast = lib.node_by_output(graph, "$(B)/generated/robot/Fast_ros_converter.h")
        self.assertEqual(len(fast["cmds"]), 2)
        self.assertIn("--has-zero-copy-peer", fast["cmds"][0]["cmd_args"])
        self.assertEqual(fast["cmds"][1]["cmd_args"][2], "cpp_zero_copy")
        self.assertEqual(len(fast["outputs"]), 5)
        query = lib.node_by_output(graph, "$(B)/generated/robot/QueryRequest.cc")
        self.assertEqual(len(query["outputs"]), 5)
        self.assertEqual(len([n for n in graph["graph"] if n.get("kv", {}).get("p") == "RS"]), 3)

    def test_consumed_headers_retain_the_generator_main_output(self):
        for source, options, header, main in (
            ("Plain.msg", "", "Plain.h", "Plain.cc"),
            ("Fast.msg", "ZERO_COPY", "Fast_zero_copy.h", "Fast.cc"),
            ("Query.srv", "", "QueryResponse.h", "Query.h"),
        ):
            with self.subTest(header=header):
                files = ros_files()
                files.update({
                    "msgs/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ROS_DIR(msgs)\nROS_SRCS({options} {source})\nEND()\n",
                    f"msgs/{source}": "int32 value\n" + ("---\nint32 result\n" if source.endswith(".srv") else ""),
                    "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(msgs)\nSRCS(main.cpp)\nEND()\n",
                    "app/main.cpp": f'#include <msgs/{header}>\nint main() {{}}\n',
                })
                graph = lib.make(files, "app")
                generated = lib.node_by_output(graph, f"$(B)/msgs/{header}")
                compiled = lib.node_by_output(graph, "$(B)/app/main.cpp.o")
                self.assertEqual(generated["outputs"][0], f"$(B)/msgs/{main}")
                self.assertIn(f"$(B)/msgs/{main}", compiled["inputs"])
                self.assertIn(generated["uid"], compiled["deps"])

    def test_message_types_resolve_peers_and_induce_generated_headers(self):
        files = ros_files()
        files.update({
            "dep/ya.make": f"ROS_LIBRARY(other)\n{lib.BARE_MODULE}\nROS_DIR(dep)\nROS_SRCS(msg/Child.msg)\nEND()\n",
            "dep/msg/Child.msg": "float64 value\n",
            "msgs/ya.make": f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}\nROS_DIR(msgs)\nPEERDIR(dep)\nROS_SRCS(msg/Parent.msg)\nEND()\n",
            "msgs/msg/Parent.msg": "enum State uint8\nState state\nOptional<other/Child[]> child # comment\n",
            "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}\nPEERDIR(msgs)\nSRCS(main.cpp)\nEND()\n",
            "app/main.cpp": '#include <msgs/Parent.h>\nint main(){}\n',
        })
        graph = lib.make(files, "app")
        parent = lib.node_by_output(graph, "$(B)/msgs/Parent.h")
        child = lib.node_by_output(graph, "$(B)/dep/Child.h")
        self.assertIn("$(S)/dep/msg/Child.msg", parent["inputs"])
        compiled = lib.node_by_output(graph, "$(B)/app/main.cpp.o")
        self.assertIn(parent["uid"], compiled["deps"])
        self.assertIn(child["uid"], compiled["deps"])
        self.assertIn("$(B)/msgs/Parent.cc", compiled["inputs"])
        self.assertIn("$(B)/dep/Child.cc", compiled["inputs"])
        self.assertNotIn("$(S)/msgs/msg/Parent.msg", compiled["inputs"])
        self.assertNotIn("$(S)/dep/msg/Child.msg", compiled["inputs"])


if __name__ == "__main__":
    unittest.main()
