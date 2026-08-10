#!/usr/bin/env python3
"""Download one immutable validation resource into a declared build output."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import tempfile
import time
import urllib.request
from pathlib import Path


SANDBOX_API = "https://sandbox.yandex-team.ru/api/v1.0/resource/"
MDS_PREFIX = "http://storage-int.mds.yandex.net/get-sandbox/"


def oauth_token() -> str:
    token = os.environ.get("YA_TOKEN", "").strip()
    if token:
        return token
    try:
        return (Path.home() / ".ya_token").read_text().strip()
    except OSError:
        return ""


def sandbox_sources(url: str, token: str) -> list[tuple[str, bool]]:
    """Return direct plus API-derived immutable download locations."""
    direct = (url, bool(token))
    resource = url.rstrip("/").rsplit("/", 1)[-1]
    if not token or not resource.isdigit():
        return [direct]
    request = urllib.request.Request(
        SANDBOX_API + resource,
        headers={"Authorization": "OAuth " + token, "User-Agent": "ay-build-validator/1"},
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            info = json.load(response)
    except Exception:  # noqa: BLE001 - the direct source may still work
        return [direct]
    if info.get("state") != "READY":
        return [direct]
    sources = []
    mds = info.get("attributes", {}).get("mds")
    if mds:
        sources.append((MDS_PREFIX + mds, False))
    proxy = info.get("http", {}).get("proxy")
    if proxy:
        separator = "&" if "?" in proxy else "?"
        proxy += separator + "origin=fetch-from-sandbox"
        if info.get("multifile"):
            proxy += "&stream=tgz"
        sources.append((proxy, True))
    sources.append(direct)
    # Preserve order while removing duplicate URLs.
    return list(dict.fromkeys(sources))


def download(url: str, output: Path, expected_sha256: str, attempts: int = 4) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    token = oauth_token()

    last_error: Exception | None = None
    for source_url, authenticated in sandbox_sources(url, token):
        headers = {"User-Agent": "ay-build-validator/1"}
        if authenticated and token:
            headers["Authorization"] = "OAuth " + token
        for attempt in range(1, attempts + 1):
            descriptor, temporary_name = tempfile.mkstemp(
                prefix="." + output.name + ".",
                dir=output.parent,
            )
            temporary = Path(temporary_name)
            digest = hashlib.sha256()
            try:
                request = urllib.request.Request(source_url, headers=headers)
                with os.fdopen(descriptor, "wb") as destination, urllib.request.urlopen(
                    request,
                    timeout=120,
                ) as source:
                    while chunk := source.read(1024 * 1024):
                        destination.write(chunk)
                        digest.update(chunk)
                    destination.flush()
                    os.fsync(destination.fileno())
                actual = digest.hexdigest()
                if expected_sha256 and actual != expected_sha256:
                    raise ValueError(
                        f"sha256 mismatch for {url}: got {actual}, want {expected_sha256}"
                    )
                os.replace(temporary, output)
                print(f"fetched resource -> {output} sha256={actual}", flush=True)
                return
            except Exception as error:  # noqa: BLE001 - retry and surface final cause
                last_error = error
                try:
                    os.close(descriptor)
                except OSError:
                    pass
                temporary.unlink(missing_ok=True)
                if attempt != attempts:
                    time.sleep(min(2 ** (attempt - 1), 8))
    assert last_error is not None
    raise last_error


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("url")
    parser.add_argument("output")
    parser.add_argument("sha256", nargs="?", default="-")
    args = parser.parse_args()
    checksum = "" if args.sha256 in ("", "-") else args.sha256.lower()
    if checksum and (len(checksum) != 64 or any(c not in "0123456789abcdef" for c in checksum)):
        parser.error("sha256 must be 64 lowercase hexadecimal characters or '-'")
    download(args.url, Path(args.output), checksum)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
