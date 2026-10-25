import argparse
import json
import os
from pathlib import Path
import sys
import unittest


def main():
    parser = argparse.ArgumentParser(description="Run make graph tests against a real Arcadia")
    parser.add_argument("--backend", choices=("ay", "ya"), required=True)
    parser.add_argument("--arcadia", required=True)
    parser.add_argument("--binary")
    parser.add_argument("--overlay-binary", type=Path, help="ay binary providing dev overlay for upstream ya")
    parser.add_argument("--parent-mount-namespace", help=argparse.SUPPRESS)
    parser.add_argument("--work-dir", type=Path, required=True)
    parser.add_argument("--result-file", type=Path, help="collect test status for the build gate; assertion failures do not fail collection")
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
    if args.backend == "ya":
        namespace = os.readlink("/proc/self/ns/mnt")
        if args.parent_mount_namespace:
            if namespace == args.parent_mount_namespace:
                parser.error("ya tests must run inside a private mount namespace")
            # Arc's metadata symlink points outside the overlay to the live
            # mount's database. An arc status in this view can deadlock that
            # database against FUSE. Fixtures are not an Arc working copy.
            (arcadia / ".arc").unlink(missing_ok=True)
        else:
            if not args.overlay_binary:
                parser.error("--overlay-binary is required for ya")
            overlay = str(args.overlay_binary.resolve())
            os.execv(overlay, [
                overlay, "dev", "overlay", str(arcadia), "--",
                sys.executable, "-m", "tst.make", "--parent-mount-namespace", namespace,
                *sys.argv[1:],
            ])
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
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    if args.result_file:
        args.result_file.parent.mkdir(parents=True, exist_ok=True)
        args.result_file.write_text(json.dumps({
            "modules": modules,
            "tests": result.testsRun,
            "success": result.wasSuccessful(),
            "failures": [{"test": str(test), "traceback": trace} for test, trace in result.failures],
            "errors": [{"test": str(test), "traceback": trace} for test, trace in result.errors],
        }, indent=2))
        return 0
    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    raise SystemExit(main())
