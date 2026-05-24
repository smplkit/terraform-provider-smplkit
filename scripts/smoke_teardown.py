#!/usr/bin/env python3
"""Tear down the ephemeral smoke account.

Called by the release-smoke job as a `if: always()` step so the account
is removed whether the apply/destroy succeeded or not. The smoke
bootstrap stashes ACCOUNT_TOKEN; we DELETE /api/v1/accounts/current
with that token. 404 is treated as success (the account is already
gone — equivalent to a successful purge).
"""
from __future__ import annotations

import argparse
import os
import sys
import urllib.error
import urllib.request


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--base-url",
        default=os.environ.get("SMOKE_BASE_URL", "https://app.smplkit.com"),
        help="App-service base URL (no trailing slash). Defaults to production.",
    )
    args = parser.parse_args()

    token = os.environ.get("ACCOUNT_TOKEN", "")
    if not token:
        print("ACCOUNT_TOKEN empty; nothing to tear down", file=sys.stderr)
        return 0

    url = f"{args.base_url}/api/v1/accounts/current"
    req = urllib.request.Request(url, method="DELETE")
    req.add_header("Authorization", f"Bearer {token}")
    req.add_header("Accept", "application/vnd.api+json")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            print(f"DELETE {url} -> {resp.status}")
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            print(f"account already gone ({exc.code})")
            return 0
        body = exc.read().decode("utf-8", errors="replace")
        print(f"DELETE {url} failed: {exc.code} {body}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
