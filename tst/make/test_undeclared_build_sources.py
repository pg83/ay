from tst.make.lib import MakeTest


class UndeclaredBuildSourcesTest(MakeTest):
    def test_external_build_root_flatbuffer_sources_can_be_consumed_without_local_producers(self):
        self.write("ya.make", "LIBRARY()\nSRCS(${ARCADIA_BUILD_ROOT}/__ay_missing_e2e__.fbs)\nEND()\n")
        generated = [node for node in self.graph()["graph"] if "$(B)/__ay_missing_e2e__.fbs.h" in node["outputs"]]
        self.assertEqual(len(generated), 1)
        self.assertIn("$(B)/__ay_missing_e2e__.fbs", generated[0]["inputs"])

    def test_build_root_python_bytecode_sources_require_a_producer(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(${ARCADIA_BUILD_ROOT}/__ay_missing_e2e__.py)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_build_root_python_resource_sources_require_a_producer_without_bytecode(self):
        self.write("ya.make", "PY3_LIBRARY()\nENABLE(PYBUILD_NO_PYC)\nPY_SRCS(${ARCADIA_BUILD_ROOT}/__ay_missing_e2e__.py)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)
