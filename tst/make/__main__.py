import argparse
import contextlib
import fcntl
import hashlib
import os
from pathlib import Path
import unittest


def main():
    parser = argparse.ArgumentParser(description="Run make graph tests against a real Arcadia")
    parser.add_argument("--backend", choices=("ay", "ya"), required=True)
    parser.add_argument("--arcadia", required=True)
    parser.add_argument("--binary")
    parser.add_argument("--work-dir", type=Path, required=True)
    parser.add_argument("modules", nargs="*")
    args = parser.parse_args()
    if not args.arcadia:
        parser.error("make tests require -Darcadia=/path/to/arcadia")
    arcadia = Path(args.arcadia).resolve()
    if not (arcadia / ".arcadia.root").exists() or not (arcadia / "build/ya.conf.json").is_file():
        parser.error(f"not an Arcadia tree: {arcadia}")
    binary = Path(args.binary).resolve() if args.binary else arcadia / "ya"
    if args.backend == "ay" and not args.binary:
        parser.error("--binary is required for ay")
    if not binary.is_file():
        parser.error(f"binary not found: {binary}")
    args.work_dir.mkdir(parents=True, exist_ok=True)
    os.environ.update(
        AY_E2E_BACKEND=args.backend,
        AY_E2E_ARCADIA=str(arcadia),
        AY_E2E_BINARY=str(binary),
        AY_E2E_WORK_DIR=str(args.work_dir.resolve()),
    )
    modules = args.modules or [
        "tst.make." + path.stem for path in sorted(Path(__file__).parent.glob("test_*.py"))
    ]
    suite = unittest.defaultTestLoader.loadTestsFromNames(modules)
    # Also serialize independent invocations against the same Arcadia. The
    # lock lives outside the tree and is never unlinked while a waiter exists.
    with contextlib.ExitStack() as stack:
        if args.backend == "ya":
            lock_key = hashlib.sha256(os.fsencode(arcadia)).hexdigest()
            lock_path = Path.home() / ".ya/ay-e2e-locks" / (lock_key + ".lock")
            lock_path.parent.mkdir(parents=True, exist_ok=True)
            lock = stack.enter_context(lock_path.open("a"))
            fcntl.flock(lock, fcntl.LOCK_EX)
        result = unittest.TextTestRunner(verbosity=2).run(suite)
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
