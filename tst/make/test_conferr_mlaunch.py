from tst.make.lib import MakeTest


class MlaunchConfigureErrorsTest(MakeTest):
    """Configure errors of the mlaunch plugin (build/internal/plugins/mlaunch.py)."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def test_node_builder_with_an_odd_option_list(self):
        self.library("NODE_BUILDER(e2e::Build NAME)")
        self.assertConfigureLikeYa()

    def test_node_builder_with_an_unknown_option(self):
        self.library("NODE_BUILDER(e2e::Build NAME node E2E value)")
        self.assertConfigureLikeYa()

    def test_node_builder_without_required_options(self):
        self.library("NODE_BUILDER(e2e::Build NAME node)")
        self.assertConfigureLikeYa()

    def test_second_node_builder(self):
        self.library("NODE_BUILDER(e2e::Build NAME node TARGET e2e/target HEADER node.h)", "NODE_BUILDER(e2e::Other NAME other TARGET e2e/target HEADER other.h)")
        self.assertConfigureLikeYa()

    def test_node_builder_with_an_unqualified_symbol(self):
        self.library("NODE_BUILDER(e2e-build NAME node TARGET e2e/target HEADER node.h)")
        self.assertConfigureLikeYa()

    def test_node_builder_with_a_negative_spinner_count(self):
        self.library("NODE_BUILDER(e2e::Build NAME node TARGET e2e/target HEADER node.h ROS_SPINNER_THREADS -1)")
        self.assertConfigureLikeYa()

    def test_node_builder_with_an_invalid_name(self):
        self.library("NODE_BUILDER(e2e::Build NAME 1node TARGET e2e/target HEADER node.h)")
        self.assertConfigureLikeYa()

    def test_node_builder_outside_of_an_mlaunch_component(self):
        self.library("NODE_BUILDER(e2e::Build NAME node TARGET e2e/target HEADER node.h)")
        self.assertConfigureLikeYa()

    def test_compound_node_builder_with_an_odd_option_list(self):
        self.library("COMPOUND_NODE_BUILDER(e2e::Build NAME)")
        self.assertConfigureLikeYa()

    def test_compound_node_builder_with_an_unknown_option(self):
        self.library("COMPOUND_NODE_BUILDER(e2e::Build NAME node ROS_SPINNER_THREADS 1)")
        self.assertConfigureLikeYa()

    def test_compound_node_builder_without_required_options(self):
        self.library("COMPOUND_NODE_BUILDER(e2e::Build NAME node)")
        self.assertConfigureLikeYa()

    def test_compound_node_builder_with_an_unqualified_symbol(self):
        self.library("COMPOUND_NODE_BUILDER(e2e-build NAME node TARGET e2e/target HEADER node.h)")
        self.assertConfigureLikeYa()

    def test_compound_node_builder_with_an_invalid_name(self):
        self.library("COMPOUND_NODE_BUILDER(e2e::Build NAME 1node TARGET e2e/target HEADER node.h)")
        self.assertConfigureLikeYa()
