from tst.make.lib import MakeTest, PROJECT


class CachedDataRetryTest(MakeTest):
    def test_cached_data_reads_retry_interrupted_system_calls(self):
        self.write("ya.make", f"EXECTEST()\nDATA(arcadia/{PROJECT}/value.txt)\nRUN(echo answer)\nEND()\n")
        self.write("value.txt", "42\n")
        self.node(self.graph("-t", "--cache-tests", env={"AY_CHAOS": "eintr-test-data=0"}),
                  "test-results/exectest/meta.json")
