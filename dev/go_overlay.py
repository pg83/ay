#!/usr/bin/env python3

import argparse
import json
from pathlib import Path


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True)
    parser.add_argument("--source-root", required=True)
    parser.add_argument("generated", nargs="+")
    args = parser.parse_args()

    source_root = Path(args.source_root)
    replacements = {
        str(source_root / Path(generated).name): str(Path(generated))
        for generated in args.generated
    }
    Path(args.output).write_text(
        json.dumps({"Replace": replacements}, sort_keys=True),
        encoding="utf-8",
    )


if __name__ == "__main__":
    main()
