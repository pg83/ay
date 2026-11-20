import argparse
import json
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description="Fail when any collected make test failed")
    parser.add_argument("--stamp", type=Path, required=True)
    parser.add_argument("results", nargs="+", type=Path)
    args = parser.parse_args()
    failed = 0
    total = 0
    skipped = 0
    xfailed = 0
    for path in args.results:
        result = json.loads(path.read_text())
        total += result["tests"]
        skipped += len(result.get("skipped", []))
        expected = result.get("expected_failures", [])
        xfailed += len(expected)
        failed += not result["success"]
        xfail_note = f", {len(expected)} expected failures" if expected else ""
        print(f"{'PASS' if result['success'] else 'FAIL'} {', '.join(result['modules'])}: {result['tests']} tests{xfail_note}")
        for error in [*result["failures"], *result["errors"]]:
            print(error["test"] + "\n" + error["traceback"])
    print(f"{total} tests, {skipped} skipped, {xfailed} expected failures, {failed} failed modules")
    if failed:
        return 1
    args.stamp.parent.mkdir(parents=True, exist_ok=True)
    args.stamp.touch()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
