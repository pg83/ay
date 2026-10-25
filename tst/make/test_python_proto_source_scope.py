import hashlib
import unittest

from tst import lib
from tst.make.test_proto_py2_context import proto_world
from tst.make.test_python_proto import args


MODULE = "citrine/binding"
SOURCE = "parcel/rpc/service.proto"


def fixture(body, version=3, module_type=None):
    files = proto_world()
    files[MODULE + "/ya.make"] = f"{module_type or f'PY{version}_LIBRARY'}()\n" \
        + lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\nNO_MYPY()\nGRPC()\n" \
        + f"PROTO_NAMESPACE({MODULE})\nPY_SRCS(TOP_LEVEL anchor.py)\n" + body + "\nEND()\n"
    files[MODULE + "/anchor.py"] = ""
    files[MODULE + "/" + SOURCE] = 'syntax = "proto2";\nimport "citrine/binding/dependency.proto";\n'
    for name in ("dependency.proto", "second.proto", "third.proto", "sub-dir/order-event.proto"):
        files[MODULE + "/" + name] = 'syntax = "proto2";\n'
    return files


def all_args(graph):
    return [a for node in graph["graph"] for cmd in node["cmds"] for a in cmd["cmd_args"]]


class PythonProtoSourceScopeTest(unittest.TestCase):
    def check_binding(self, graph, source, logical, version=3, physical=None):
        stem = physical or MODULE + "/" + source.removesuffix(".proto")
        outputs = [f"$(B)/{stem}__intpy{version}__{s}" for s in ("_pb2.py", "_pb2_grpc.py")]
        producer = lib.node_by_output(graph, outputs[0])
        self.assertEqual(producer["outputs"], outputs)
        packed_args = all_args(graph)
        for suffix in ("_pb2", "_pb2_grpc"):
            module = logical + suffix
            key = "resfs/file/py/" + module.replace(".", "/") + ".py" if version == 3 else "/py_modules/" + module
            self.assertTrue(any(key in arg for arg in packed_args), key)
        grpc_output = outputs[1]
        bytecode = next(n for n in graph["graph"] if grpc_output in args(n)
            and any(o.endswith(".yapyc3" if version == 3 else ".yapyc") for o in n["outputs"]))
        self.assertIn(outputs[0], bytecode["inputs"])
        self.assertIn(grpc_output, bytecode["inputs"])
        self.assertIn(producer["uid"], bytecode["deps"])
        resources = [n for n in graph["graph"] if bytecode["outputs"][0] in n["inputs"]]
        self.assertTrue(resources)
        self.assertTrue(any(bytecode["uid"] in n["deps"] for n in resources))
        self.assertTrue(all(o.startswith("$(B)/") for n in graph["graph"] for o in n["outputs"]))
        return producer

    def test_top_level_changes_logical_keys_without_renaming_generated_files(self):
        files = fixture(f"PY_SRCS(TOP_LEVEL parcel/__init__.py {SOURCE})")
        files[MODULE + "/parcel/__init__.py"] = ""
        graph = lib.make(files, MODULE, "--sandboxing")
        producer = self.check_binding(graph, SOURCE, "parcel.rpc.service")
        self.assertEqual(producer["cmds"][0]["cwd"], "$(S)")
        self.assertIn(f"/{MODULE}", args(producer))
        self.assertIn(f"--python_out=$(B)/{MODULE}", args(producer))
        for source in ("$(S)/" + MODULE + "/" + SOURCE, "$(S)/" + MODULE + "/dependency.proto"):
            self.assertIn(source, producer["inputs"])
        for tool in ("contrib/tools/protoc/protoc", "contrib/tools/protoc/plugins/grpc_python/grpc_python"):
            node = lib.node_by_output(graph, "$(B)/" + tool)
            self.assertIn(node["outputs"][0], producer["inputs"])
            self.assertIn(node["uid"], producer["deps"])
        self.assertFalse(any("resfs/file/py/citrine/binding/parcel/rpc/service_pb2" in a for a in all_args(graph)))

    def test_default_python_namespace_is_independent_of_the_protoc_output_namespace(self):
        files = fixture(f"PY_SRCS({SOURCE})")
        files[MODULE + "/ya.make"] = files[MODULE + "/ya.make"].replace(
            f"PROTO_NAMESPACE({MODULE})", "PROTO_NAMESPACE(citrine)")
        graph = lib.make(files, MODULE, "--sandboxing")
        self.check_binding(graph, SOURCE, "citrine.binding.parcel.rpc.service")

    def test_namespace_spelling_is_used_verbatim_before_module_to_path_conversion(self):
        for namespace in ("visible.api.", "."):
            with self.subTest(namespace=namespace):
                graph = lib.make(fixture(f"PY_SRCS(NAMESPACE {namespace} {SOURCE})"), MODULE, "--sandboxing")
                self.check_binding(graph, SOURCE, namespace + ".parcel.rpc.service")

    def test_each_input_keeps_its_scope_and_new_calls_reset_to_module_namespace(self):
        body = f"PY_NAMESPACE(module.api)\nPY_SRCS(TOP_LEVEL {SOURCE} NAMESPACE public.v2 second.proto)\nPY_SRCS(third.proto)"
        graph = lib.make(fixture(body), MODULE, "--sandboxing")
        for source, logical in ((SOURCE, "parcel.rpc.service"), ("second.proto", "public.v2.second"),
                                ("third.proto", "module.api.third")):
            self.check_binding(graph, source, logical)

    def test_explicit_proto_aliases_bypass_namespace_and_allow_empty_modules(self):
        for alias in ("published.entry", "", "literal/alias"):
            with self.subTest(alias=alias):
                graph = lib.make(fixture(f"PY_SRCS(NAMESPACE ignored.scope {SOURCE}={alias})"), MODULE, "--sandboxing")
                self.check_binding(graph, SOURCE, alias)
                self.assertEqual([a for a in all_args(graph) if a.startswith("py/namespace/")], [
                    f"py/namespace/{hashlib.md5(b'anchor').hexdigest()}/{MODULE}=.",
                ])

    def test_distinct_aliases_reuse_one_physical_proto_and_bytecode_producer(self):
        body = f"PY_SRCS({SOURCE}=public.one {SOURCE}=public.two)\nPY_SRCS({SOURCE}=public.three)"
        graph = lib.make(fixture(body), MODULE, "--sandboxing")
        for alias in ("public.one", "public.two", "public.three"):
            self.check_binding(graph, SOURCE, alias)
        output = f"$(B)/{MODULE}/parcel/rpc/service__intpy3___pb2.py"
        self.assertEqual(sum(output in n["outputs"] for n in graph["graph"]), 1)
        self.assertEqual(sum(n["kv"].get("p") == "PY" and output in n["inputs"]
                            and n["outputs"][0].startswith(output + ".") for n in graph["graph"]), 1)

    def test_source_resolution_does_not_replace_declared_python_module_name(self):
        files = fixture("SRCDIR(shared/input)\nPY_SRCS(TOP_LEVEL remote.proto)")
        files["shared/input/remote.proto"] = 'syntax = "proto2";\n'
        graph = lib.make(files, MODULE, "--sandboxing")
        producer = self.check_binding(graph, "remote.proto", "remote", physical="shared/input/remote")
        self.assertIn("$(S)/shared/input/remote.proto", producer["inputs"])
        self.assertNotIn("$(S)/citrine/binding/remote.proto", producer["inputs"])

    def test_build_root_proto_alias_preserves_the_original_producer_obligation(self):
        files = fixture("RUN_PROGRAM(tools/generate IN seed.txt OUT_NOAUTO marker.bin generated.proto OUTPUT_INCLUDES citrine/binding/dependency.proto)\n"
            "PY_SRCS(TOP_LEVEL ${BINDIR}/generated.proto=published.generated)")
        lib.tool_program(files, "tools/generate", "generate")
        files[MODULE + "/seed.txt"] = "generator seed\n"
        graph = lib.make(files, MODULE, "--sandboxing")
        generator = lib.node_by_output(graph, "$(B)/" + MODULE + "/generated.proto")
        producer = self.check_binding(graph, "generated.proto", "published.generated")
        self.assertEqual(producer["cmds"][0]["cwd"], "$(B)")
        self.assertIn(generator["uid"], producer["deps"])
        for output in generator["outputs"]:
            self.assertIn(output, producer["inputs"])
        self.assertIn("$(S)/" + MODULE + "/dependency.proto", producer["inputs"])
        self.assertIn("$(S)/" + MODULE + "/seed.txt", generator["inputs"])
        self.assertIn("$(B)/tools/generate/generate", generator["inputs"])
        self.assertNotIn("$(S)/" + MODULE + "/generated.proto", producer["inputs"])

    def test_python2_uses_dotted_logical_keys_with_the_old_physical_compiler(self):
        for scope, logical in (("TOP_LEVEL", "parcel.rpc.service"), ("NAMESPACE visible.api", "visible.api.parcel.rpc.service")):
            with self.subTest(scope=scope):
                graph = lib.make(fixture(f"PY_SRCS({scope} {SOURCE})", version=2), MODULE, "--sandboxing")
                producer = self.check_binding(graph, SOURCE, logical, version=2)
                self.assertIn("$(B)/contrib/tools/protoc_old/protoc_old", args(producer))

    def test_python2_alias_preserves_its_logical_spelling(self):
        graph = lib.make(fixture(f"PY_SRCS(NAMESPACE ignored {SOURCE}=literal/alias)", version=2), MODULE, "--sandboxing")
        self.check_binding(graph, SOURCE, "literal/alias", version=2)

    def test_an_unrecognized_alias_never_becomes_a_proto_producer(self):
        files = fixture("PY_SRCS(unrecognized.txt=unrelated.module)")
        result = lib.make_process(files, MODULE, "--sandboxing")
        self.assertNotIn("panic:", result.stderr)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("in PY_SRCS: unrecognized arg", result.stderr)
        self.assertIn("unrecognized.txt", result.stderr)

    def test_only_proto_library_derived_names_normalize_hyphens(self):
        for module_type, logical in (("PY3_LIBRARY", "sub-dir.order-event"), ("PROTO_LIBRARY", "sub_dir.order_event")):
            with self.subTest(module_type=module_type):
                files = fixture("EXCLUDE_TAGS(CPP_PROTO)\nPY_SRCS(TOP_LEVEL sub-dir/order-event.proto)", module_type=module_type)
                files["app/ya.make"] = "PY3_PROGRAM()\n" + lib.BARE_MODULE + f"PEERDIR({MODULE})\nEND()\n"
                graph = lib.make(files, "app", "--sandboxing")
                self.check_binding(graph, "sub-dir/order-event.proto", logical)

    def test_legacy_srcs_protobuf_context_keeps_each_python_version_namespace(self):
        for version in (2, 3):
            with self.subTest(version=version):
                files = fixture(f"EXCLUDE_TAGS(CPP_PROTO)\nPY_NAMESPACE(legacy.api)\nSRCS({SOURCE})",
                                version=version, module_type="PROTO_LIBRARY")
                files["client/ya.make"] = f"PY{version}_PROGRAM()\n" + lib.BARE_MODULE \
                    + f"NO_CHECK_IMPORTS()\nPEERDIR({MODULE})\nEND()\n"
                graph = lib.make(files, "client", "--sandboxing")
                self.check_binding(graph, SOURCE, "legacy.api.parcel.rpc.service", version=version)

    def test_shared_lexical_include_keeps_the_module_context_scope(self):
        files = fixture("")
        files["shared/protos.inc"] = "PY_SRCS(NAMESPACE ${VISIBLE} packet.proto)\n"
        for directory, namespace in ((MODULE, "visible.one"), ("citrine/second", "visible.two")):
            files[directory + "/ya.make"] = "PY3_LIBRARY()\n" + lib.BARE_MODULE \
                + f"NO_MYPY()\nGRPC()\nPY_SRCS(TOP_LEVEL anchor.py)\nSET(VISIBLE {namespace})\n" \
                + "INCLUDE(${ARCADIA_ROOT}/shared/protos.inc)\nEND()\n"
            files[directory + "/anchor.py"] = ""
            files[directory + "/packet.proto"] = 'syntax = "proto2";\n'
        graph = lib.make(files, MODULE, "citrine/second", "--sandboxing")
        self.check_binding(graph, "packet.proto", "visible.one.packet")
        self.check_binding(graph, "packet.proto", "visible.two.packet", physical="citrine/second/packet")


if __name__ == "__main__":
    unittest.main(verbosity=2)
