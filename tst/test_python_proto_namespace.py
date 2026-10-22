import unittest

import lib
from test_proto_py2_context import proto_world
from test_python_proto import args


class PythonProtoNamespaceTest(unittest.TestCase):
    def test_proto_module_names_normalize_hyphens_without_renaming_files(self):
        module = "wire-models/event-api"
        source = "sub-dir/order-event.proto"
        for namespace, prefix in ((None, "wire_models/event_api/"),
                                  ("public-api.events-v2", "public_api/events_v2/"),
                                  (".", "")):
            with self.subTest(namespace=namespace):
                files = proto_world()
                for target, version in (("py", "PY2"), ("py3", "PY3")):
                    files[f"{target}/ya.make"] = (
                        f"{version}_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR({module})\nEND()\n"
                    )
                namespace_stmt = f"PY_NAMESPACE({namespace})\n" if namespace is not None else ""
                files[f"{module}/ya.make"] = (
                    f"PROTO_LIBRARY()\n{lib.BARE_MODULE}GRPC()\nNO_MYPY()\nNO_OPTIMIZE_PY_PROTOS()\n"
                    f"{namespace_stmt}SRCS({source})\nEND()\n"
                )
                files[f"{module}/{source}"] = 'syntax = "proto2";\nmessage Event {}\n'
                graph = lib.make(files, "py", "py3")
                for version in ("py2", "py3"):
                    with self.subTest(version=version):
                        stem = f"{module}/sub-dir/order-event__int{version}__"
                        outputs = [f"$(B)/{stem}{suffix}" for suffix in ("_pb2.py", "_pb2_grpc.py")]
                        producer = lib.node_by_output(graph, outputs[0])
                        self.assertEqual(producer["outputs"], outputs)
                        self.assertIn(f"$(S)/{module}/{source}", producer["inputs"])
                        self.assertIn(f"{module}/{source}", args(producer))
                        resources = next(n for n in graph["graph"] if n["kv"].get("p") == "PR"
                                         and all(out in n["inputs"] for out in outputs))
                        self.assertIn(producer["uid"], resources["deps"])
                        for suffix in ("_pb2", "_pb2_grpc"):
                            logical = f"{prefix}sub_dir/order_event{suffix}"
                            physical = f"{stem}{suffix}.py"
                            if version == "py2":
                                key = logical.replace("/", ".")
                                expected = f"resfs/src//py_modules/{key}={physical}"
                            else:
                                expected = f"resfs/src/resfs/file/py/{logical}.py={physical}"
                            self.assertIn(expected, args(resources))


if __name__ == "__main__":
    unittest.main()
