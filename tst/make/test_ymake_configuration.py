import copy
import json
import re

from tst.make import toolchains
from tst.make.lib import MakeTest


class YmakeConfigurationTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nCFLAGS(-DLOCAL=${E2E_LOCAL} -DVALUE=${E2E_VALUE})\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.cxx_files = toolchains.cxx_files(self)
        self.default_file = toolchains.default_linux_clang(self)[0]

    def cxx_copy(self):
        return copy.deepcopy(self.cxx_files)

    def test_local_ymake_flags_and_ignored_lines(self):
        self.write_tree("local.ymake", "\n# ignored comment\nnot an assignment\nSANITIZER_TYPE = forbidden\nE2E_VALUE = with=equals\nE2E_LOCAL = local\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-DLOCAL=local", args)
        self.assertIn("-DVALUE=with=equals", args)

    def test_generated_configuration_appends_and_stderr(self):
        script = self.root / "build/ymake_conf.py"
        self.write_tree("build/ymake_conf.py", script.read_text() + '''
import sys
sys.stderr.write("e2e configuration diagnostic\\n")
print("E2E_CONF_FLAGS=-DE2E_CONF_FIRST")
print("E2E_CONF_FLAGS+=-DE2E_CONF_SECOND")
print("E2E_CONF_FLAGS+=")
print('when ($OS_LINUX == "yes") {')
print("    E2E_CONF_FLAGS+=-DE2E_CONF_THIRD")
print("}")
''')
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nCFLAGS(${E2E_CONF_FLAGS})\nEND()\n")
        args = [arg for command in self.node(self.graph(), "answer.cpp.o")["cmds"] for arg in command["cmd_args"]]
        for flag in ("FIRST", "SECOND", "THIRD"):
            self.assertIn("-DE2E_CONF_" + flag, args)

    def test_failed_configuration_generator_is_reported(self):
        self.write_tree("build/ymake_conf.py", 'import sys\nsys.stderr.write("e2e configuration failure\\n")\nraise SystemExit(42)\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_invalid_configuration_output_is_rejected(self):
        script = self.root / "build/ymake_conf.py"
        self.write_tree("build/ymake_conf.py", script.read_text() + '\nprint("=e2e_invalid_assignment")\n')
        self.assertNotEqual(self.make().returncode, 0)

    def test_missing_configuration_generator_is_reported(self):
        self.write_tree("build/ymake_conf.py", "")
        (self.root / "build/ymake_conf.py").unlink()
        self.assertNotEqual(self.make().returncode, 0)

    def test_toolchain_configuration_sections_require_objects(self):
        for field, value in (("toolchains", []), ("toolchains", "wrong"), ("bottles", []), ("bottles", True), (None, [])):
            with self.subTest(field=field, value=value):
                config = self.cxx_copy()[self.default_file]
                if field is None:
                    config = value
                else:
                    config[field] = value
                toolchains.write(self, {self.default_file: config})
                self.assertNotEqual(self.make().returncode, 0)

    def test_toolchains_require_host_and_cxx_tool_description(self):
        for invalid in ("host", "cxx", "absent"):
            with self.subTest(invalid=invalid):
                files = self.cxx_copy()
                for _, _, entry in toolchains.cxx_entries(files):
                    if invalid == "host":
                        for platform in entry["platforms"]:
                            platform["host"] = {}
                    elif invalid == "cxx":
                        entry["tools"]["c++"] = None
                    else:
                        del entry["tools"]["c++"]
                toolchains.write(self, files)
                self.assertNotEqual(self.make().returncode, 0)

    def test_duplicate_host_key_cannot_hide_a_missing_platform(self):
        text, count = re.subn(r'"host": (\{[^{}]*\})', r'"host": \1, "host": null', json.dumps(self.cxx_files[self.default_file]))
        self.assertGreater(count, 0)
        toolchains.write(self, {self.default_file: text})
        self.assertNotEqual(self.make().returncode, 0)

    def test_toolchain_target_defaults_to_its_host(self):
        files = self.cxx_copy()
        removed = 0
        for _, _, entry in toolchains.cxx_entries(files):
            for platform in entry["platforms"]:
                if platform.get("target") == dict({"arch": "x86_64"}, **platform.get("host", {})):
                    del platform["target"]
                    removed += 1
        self.assertGreater(removed, 0)
        toolchains.write(self, files)
        self.node(self.graph(), "answer.cpp.o")

    def test_toolchain_bottles_cannot_be_missing(self):
        for bottles in (None, {}):
            with self.subTest(bottles=bottles):
                toolchains.write(self, {self.default_file: dict(self.cxx_copy()[self.default_file], bottles=bottles)})
                self.assertNotEqual(self.make().returncode, 0)

    def test_invalid_toolchain_formulas_are_configuration_errors(self):
        for formula in ("build/ay_e2e_missing_formula.json", {"by_platform": {}},
                        {"by_platform": {"E2E_UNKNOWN": {"uri": "sbr:42"}}}):
            with self.subTest(formula=formula):
                files = self.cxx_copy()
                for path, _, entry in toolchains.cxx_entries(files):
                    bottle = entry["tools"]["c++"].get("bottle")
                    if bottle:
                        files[path]["bottles"][bottle]["formula"] = formula
                toolchains.write(self, files)
                self.assertNotEqual(self.make().returncode, 0)
