import importlib.util
import pathlib
import stat
import tempfile
import unittest


SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("go_bootstrap_module", SCRIPT_DIR / "go_bootstrap.py")
GO_BOOTSTRAP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(GO_BOOTSTRAP)


class ResolveGoBinaryTest(unittest.TestCase):
    def make_executable(self, root, relative_path):
        path = pathlib.Path(root) / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR)
        return path.resolve()

    def test_prefers_ay_go_override(self):
        with tempfile.TemporaryDirectory() as tmp:
            override = self.make_executable(tmp, "toolchains/go")

            got = GO_BOOTSTRAP.resolve_go_binary(
                env={GO_BOOTSTRAP.AY_GO_ENV: str(override)},
                path="",
                candidates=(),
                glob_patterns=(),
            )

            self.assertEqual(got, str(override))

    def test_prefers_go_on_path_before_fallbacks(self):
        with tempfile.TemporaryDirectory() as tmp:
            path_go = self.make_executable(tmp, "path-bin/go")
            fallback_go = self.make_executable(tmp, "fallback/go")

            got = GO_BOOTSTRAP.resolve_go_binary(
                env={},
                path=str(path_go.parent),
                candidates=(str(fallback_go),),
                glob_patterns=(),
            )

            self.assertEqual(got, str(path_go))

    def test_uses_fallback_candidates_when_path_is_missing(self):
        with tempfile.TemporaryDirectory() as tmp:
            fallback_go = self.make_executable(tmp, "fallback/go")

            got = GO_BOOTSTRAP.resolve_go_binary(
                env={},
                path="",
                candidates=(str(fallback_go),),
                glob_patterns=(),
            )

            self.assertEqual(got, str(fallback_go))

    def test_reports_invalid_ay_go_override(self):
        with tempfile.TemporaryDirectory() as tmp:
            missing = pathlib.Path(tmp) / "missing-go"

            with self.assertRaisesRegex(FileNotFoundError, GO_BOOTSTRAP.AY_GO_ENV):
                GO_BOOTSTRAP.resolve_go_binary(
                    env={GO_BOOTSTRAP.AY_GO_ENV: str(missing)},
                    path="",
                    candidates=(),
                    glob_patterns=(),
                )


if __name__ == "__main__":
    unittest.main()
