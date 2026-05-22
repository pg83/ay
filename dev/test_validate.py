import importlib.util
import pathlib
import tempfile
import unittest


SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("validate_module", SCRIPT_DIR / "validate.py")
VALIDATE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(VALIDATE)


class NormalizedNodeParityCountsTest(unittest.TestCase):
    def write_lines(self, root, name, lines):
        path = pathlib.Path(root) / name
        path.write_text("".join(lines), encoding="utf-8")
        return path

    def test_counts_exact_matches_and_only_side_nodes(self):
        with tempfile.TemporaryDirectory() as tmp:
            left = self.write_lines(
                tmp,
                "left.jsonl",
                [
                    '{"self_uid":"A"}\n',
                    '{"self_uid":"B"}\n',
                    '{"self_uid":"D"}\n',
                ],
            )
            right = self.write_lines(
                tmp,
                "right.jsonl",
                [
                    '{"self_uid":"A"}\n',
                    '{"self_uid":"C"}\n',
                    '{"self_uid":"D"}\n',
                    '{"self_uid":"E"}\n',
                ],
            )

            got = VALIDATE.normalized_node_parity_counts(left, right)

            self.assertEqual(
                got,
                VALIDATE.ParityCounts(
                    exact=2,
                    left_only=1,
                    right_only=2,
                    left_total=3,
                    right_total=4,
                ),
            )

    def test_counts_duplicates_as_multiset_matches(self):
        with tempfile.TemporaryDirectory() as tmp:
            left = self.write_lines(
                tmp,
                "left.jsonl",
                [
                    '{"self_uid":"A"}\n',
                    '{"self_uid":"A"}\n',
                    '{"self_uid":"B"}\n',
                ],
            )
            right = self.write_lines(
                tmp,
                "right.jsonl",
                [
                    '{"self_uid":"A"}\n',
                    '{"self_uid":"C"}\n',
                ],
            )

            got = VALIDATE.normalized_node_parity_counts(left, right)

            self.assertEqual(
                got,
                VALIDATE.ParityCounts(
                    exact=1,
                    left_only=2,
                    right_only=1,
                    left_total=3,
                    right_total=2,
                ),
            )


if __name__ == "__main__":
    unittest.main()
