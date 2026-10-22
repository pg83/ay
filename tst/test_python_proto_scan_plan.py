import unittest

import lib
from test_python_proto import WRAPPER, python_base
from test_python_proto_root_includes import END_INCLUDES, includes, prefix


MODULE = "readers/fern"
OUTPUT = f"$(B)/{MODULE}/packet__intpy3___pb2.py"


def world(body):
    files = python_base()
    files.update({
        f"{MODULE}/ya.make": f"PY3_LIBRARY()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nNO_MYPY()\n{body}PY_SRCS(packet.proto)\nEND()\n",
        f"{MODULE}/packet.proto": 'syntax = "proto2";\nimport "choice.proto";\nmessage Packet {}\n',
        "imports/earlier/choice.proto": 'syntax = "proto2";\nimport "leaf.proto";\nmessage Earlier {}\n',
        "imports/earlier/leaf.proto": 'syntax = "proto2";\nmessage Leaf {}\n',
        "imports/later/choice.proto": 'syntax = "proto2";\nmessage Later {}\n',
        "wire/output/choice.proto": 'syntax = "proto2";\nmessage Output {}\n',
    })
    return files


class PythonProtoScanPlanTest(unittest.TestCase):
    def assert_choice(self, node, directory):
        self.assertIn(f"$(S)/{directory}/choice.proto", node["inputs"])
        for other in ("imports/earlier", "imports/later", "wire/output"):
            if other != directory:
                self.assertNotIn(f"$(S)/{other}/choice.proto", node["inputs"])
        self.assertIn(WRAPPER, node["inputs"])
        self.assertIn(f"$(S)/{MODULE}/packet.proto", node["inputs"])

    def test_local_and_global_collision_uses_first_declaration(self):
        for first, second in (("", "GLOBAL "), ("GLOBAL ", "")):
            with self.subTest(first=first):
                files = world(f"ADDINCL({first}FOR proto imports/earlier)\nADDINCL({second}FOR proto imports/later)\n")
                node = lib.node_by_output(lib.make(files, MODULE), OUTPUT)
                self.assertEqual(includes(node), prefix("") + [
                    "-I=$(S)/imports/earlier", "-I=$(S)/imports/later", *END_INCLUDES,
                ])
                self.assert_choice(node, "imports/earlier")
                self.assertIn("$(S)/imports/earlier/leaf.proto", node["inputs"])

    def test_python_proto_variants_use_the_same_declared_scan_plan(self):
        from test_proto_py2_context import proto_world

        files = proto_world()
        files.update(world(""))
        files[f"{MODULE}/ya.make"] = (
            f"PROTO_LIBRARY()\n{lib.BARE_MODULE}ONLY_TAGS(PY_PROTO PY3_PROTO)\nNO_MYPY()\n"
            "ADDINCL(FOR proto imports/earlier)\nADDINCL(GLOBAL FOR proto imports/later)\n"
            "SRCS(packet.proto)\nEND()\n"
        )
        for version in ("PY2", "PY3"):
            files[f"caller/{version}/ya.make"] = (
                f"{version}_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR({MODULE})\nEND()\n"
            )
        graph = lib.make(files, "caller/PY2", "caller/PY3")
        for version in ("py2", "py3"):
            node = lib.node_by_output(graph, OUTPUT.replace("py3", version))
            self.assert_choice(node, "imports/earlier")
            self.assertIn("$(S)/imports/earlier/leaf.proto", node["inputs"])

    def test_output_namespace_does_not_override_declared_scanner_includes(self):
        files = world("SET(PROTO_NAMESPACE wire/output)\nADDINCL(FOR proto imports/earlier)\n")
        node = lib.node_by_output(lib.make(files, MODULE), OUTPUT)
        self.assertEqual(includes(node), prefix("wire/output") + ["-I=$(S)/imports/earlier", *END_INCLUDES])
        self.assert_choice(node, "imports/earlier")
        self.assertIn("$(S)/imports/earlier/leaf.proto", node["inputs"])

    def test_runtime_path_is_searched_only_through_real_include_declarations(self):
        files = world("ADDINCL(FOR proto imports/earlier)\n")
        files["contrib/libs/protobuf/src/choice.proto"] = 'syntax = "proto2";\nmessage RuntimeChoice {}\n'
        node = lib.node_by_output(lib.make(files, MODULE), OUTPUT)
        self.assert_choice(node, "imports/earlier")
        self.assertNotIn("$(S)/contrib/libs/protobuf/src/choice.proto", node["inputs"])

    def test_local_source_directory_precedes_declared_and_propagated_includes(self):
        files = world("ADDINCL(FOR proto imports/earlier)\nPEERDIR(support/exporter)\n")
        files[f"{MODULE}/choice.proto"] = 'syntax = "proto2";\nmessage LocalChoice {}\n'
        files["support/exporter/ya.make"] = (
            f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(GLOBAL FOR proto imports/later)\nEND()\n"
        )
        node = lib.node_by_output(lib.make(files, MODULE), OUTPUT)
        self.assertEqual(includes(node), prefix("") + [
            "-I=$(S)/imports/earlier", "-I=$(S)/imports/later", *END_INCLUDES,
        ])
        self.assert_choice(node, MODULE)

    def test_propagated_proto_includes_follow_local_declarations(self):
        for local in ("ADDINCL(FOR proto imports/earlier)\n", ""):
            with self.subTest(local=local):
                files = world(local + "PEERDIR(support/exporter)\n")
                files["support/exporter/ya.make"] = (
                    f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(GLOBAL FOR proto imports/later)\nEND()\n"
                )
                node = lib.node_by_output(lib.make(files, MODULE), OUTPUT)
                self.assert_choice(node, "imports/earlier" if local else "imports/later")

    def test_ymaps_export_keeps_its_declared_proto_directory(self):
        files = world("EXPORT_YMAPS_PROTO()\n")
        files["maps/doc/proto/choice.proto"] = 'syntax = "proto2";\nmessage MapsChoice {}\n'
        node = lib.node_by_output(lib.make(files, MODULE), OUTPUT)
        self.assertEqual(includes(node), prefix("maps/doc/proto") + ["-I=$(S)/maps/doc/proto", *END_INCLUDES])
        self.assert_choice(node, "maps/doc/proto")

    def test_shared_lexical_declarations_keep_separate_module_scan_plans(self):
        files = world("")
        files["shared/proto.inc"] = "ADDINCL(FOR proto ${FIRST})\nADDINCL(GLOBAL FOR proto ${SECOND})\n"
        for name, first, second in (("fern", "earlier", "later"), ("moss", "later", "earlier")):
            module = f"readers/{name}"
            files[f"{module}/ya.make"] = (
                f"PY3_LIBRARY()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nNO_MYPY()\n"
                f"SET(FIRST imports/{first})\nSET(SECOND imports/{second})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/proto.inc)\nPY_SRCS(packet.proto)\nEND()\n"
            )
            files[f"{module}/packet.proto"] = files[f"{MODULE}/packet.proto"]
        graph = lib.make(files, MODULE, "readers/moss")
        for name, first, second in (("fern", "earlier", "later"), ("moss", "later", "earlier")):
            node = lib.node_by_output(graph, OUTPUT.replace("fern", name))
            self.assertEqual(includes(node), prefix("") + [f"-I=$(S)/imports/{first}", f"-I=$(S)/imports/{second}", *END_INCLUDES])
            self.assertIn(f"$(S)/imports/{first}/choice.proto", node["inputs"])
            self.assertNotIn(f"$(S)/imports/{second}/choice.proto", node["inputs"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
