from tst.make import upstream_rust
from tst.make.lib import MakeTest, PROJECT


class RustSbomTest(MakeTest):
    def setUp(self):
        super().setUp()
        upstream_rust.install(self)

    def test_licensed_crate_keeps_its_sbom_component(self):
        self.write("ya.make", "CRATE(answer)\nLICENSE(MIT)\nCRATE_VERSION(1.2.3)\nEND()\n")
        self.write("src/lib.rs", "pub fn answer() -> u32 { 42 }\n")
        graph = self.graph("--build=release")
        component = self.node(graph, "answer.RUST.component.sbom")
        crate = self.node(graph, "libanswer.a")
        self.assertIn(f"$(B)/{PROJECT}/answer.RUST.component.sbom", crate["inputs"])
        self.assertIn(component["uid"], crate["deps"])
