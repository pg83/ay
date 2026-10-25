import unittest

from tst import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
TOOL = "yt/yt/tools/record_codegen/yt_record_codegen/bin"
RUNTIME = "yt/yt/client/table_client/record_codegen_deps.h"


def files_for(body):
    files = {
        "mod/ya.make": "LIBRARY()\n" + BARE + body + "END()\n",
        "mod/records/query.yaml": "namespace: NQuery\n",
        "mod/use.cpp": '#include "mod/records/query.record.h"\n',
        "mod/public.h": '#include "detail.h"\n',
        "mod/detail.h": "",
        RUNTIME: '#include "runtime.h"\n',
        "yt/yt/client/table_client/runtime.h": "",
        "yt/yt/client/ya.make": "LIBRARY()\n" + BARE + "CFLAGS(GLOBAL -DRECORD_CLIENT)\nEND()\n",
        "other/client/ya.make": "LIBRARY()\n" + BARE + "CFLAGS(GLOBAL -DOTHER_CLIENT)\nEND()\n",
    }
    lib.tool_program(files, TOOL, "record_codegen")
    return files


class YtRecordCodegenTest(unittest.TestCase):
    def test_record_outputs_compile_and_reach_consumers(self):
        graph = lib.make(files_for(
            "SET(SCHEMA records/query.yaml)\n"
            "GENERATE_YT_RECORD(${SCHEMA} OUTPUT_INCLUDES mod/public.h)\n"
            "SRCS(use.cpp)\n"
        ), "mod")
        record = lib.node_by_output(graph, "$(B)/mod/records/query.record.h")
        self.assertEqual(record["kv"], {"p": "RC"})
        self.assertEqual(record["outputs"], [
            "$(B)/mod/records/query.record.cpp", "$(B)/mod/records/query.record.h",
        ])
        self.assertEqual(record["cmds"][0]["cmd_args"], [
            "$(B)/" + TOOL + "/record_codegen", "--input", "$(S)/mod/records/query.yaml",
            "--output-root", "$(B)", "--output-cpp", "$(B)/mod/records/query.record.cpp",
            "--output-include", "mod/public.h",
        ])
        self.assertEqual(record["inputs"], [
            "$(B)/" + TOOL + "/record_codegen", "$(S)/mod/records/query.yaml",
        ])
        tool = lib.node_by_output(graph, "$(B)/" + TOOL + "/record_codegen")
        self.assertEqual(record["foreign_deps"], {"tool": [tool["uid"]]})
        for output in ("$(B)/mod/_/records/query.record.cpp.o", "$(B)/mod/use.cpp.o"):
            compile_node = lib.node_by_output(graph, output)
            self.assertIn(record["uid"], compile_node["deps"])
            self.assertIn("$(B)/mod/records/query.record.cpp", compile_node["inputs"])
            for path in ("mod/public.h", "mod/detail.h", RUNTIME,
                         "yt/yt/client/table_client/runtime.h"):
                self.assertIn("$(S)/" + path, compile_node["inputs"])
            self.assertNotIn("$(S)/mod/records/query.yaml", compile_node["inputs"])
            self.assertIn("-DRECORD_CLIENT", compile_node["cmds"][0]["cmd_args"])
        consumer = lib.node_by_output(graph, "$(B)/mod/use.cpp.o")
        self.assertIn("$(B)/mod/records/query.record.h", consumer["inputs"])

    def test_peer_configuration_and_empty_output_includes(self):
        for setup, expected in (
            ("YT_RECORD_DISABLE_PEERDIR()\n", []),
            ("SET(YT_RECORD_PEERDIR other/client)\n", ["-DOTHER_CLIENT"]),
            ("SET(YT_RECORD_PEERDIR)\n", []),
        ):
            with self.subTest(setup=setup):
                graph = lib.make(files_for(setup + "GENERATE_YT_RECORD(records/query.yaml)\n"), "mod")
                record = lib.node_by_output(graph, "$(B)/mod/records/query.record.h")
                self.assertNotIn("--output-include", record["cmds"][0]["cmd_args"])
                compile_node = lib.node_by_output(graph, "$(B)/mod/_/records/query.record.cpp.o")
                self.assertIn("$(S)/" + RUNTIME, compile_node["inputs"])
                args = compile_node["cmds"][0]["cmd_args"]
                self.assertNotIn("-DRECORD_CLIENT", args)
                self.assertEqual([a for a in args if a == "-DOTHER_CLIENT"], expected)

    def test_rooted_and_source_directory_inputs_keep_output_binding(self):
        for setup, yaml, prefix, object_path in (
            ("", "${ARCADIA_ROOT}/schemas/query.yaml", "$(B)/schemas/query.record",
             "$(B)/mod/__/schemas/query.record.cpp.o"),
            ("SRCDIR(schemas)\n", "query.yaml", "$(B)/mod/query.record",
             "$(B)/mod/query.record.cpp.o"),
        ):
            with self.subTest(yaml=yaml):
                files = files_for(setup + "GENERATE_YT_RECORD(" + yaml + ")\n")
                files["schemas/query.yaml"] = "namespace: NQuery\n"
                graph = lib.make(files, "mod")
                record = lib.node_by_output(graph, prefix + ".h")
                args = record["cmds"][0]["cmd_args"]
                self.assertEqual(args[args.index("--input") + 1], "$(S)/schemas/query.yaml")
                self.assertEqual(args[args.index("--output-cpp") + 1], prefix + ".cpp")
                compile_node = lib.node_by_output(graph, object_path)
                self.assertIn(prefix + ".cpp", compile_node["inputs"])
                self.assertIn(record["uid"], compile_node["deps"])

    def test_argument_validation(self):
        for call in ("GENERATE_YT_RECORD()", "GENERATE_YT_RECORD(a.yaml unexpected)",
                     "YT_RECORD_DISABLE_PEERDIR(unexpected)"):
            with self.subTest(call=call):
                result = lib.make_process(files_for(call + "\n"), "mod")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("expects", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
