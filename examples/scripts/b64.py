#!/usr/bin/env python3
# Decode stdin if it looks like base64 text, otherwise encode it.
import base64
import binascii
import re
import sys

B64_RE = re.compile(r"^[A-Za-z0-9+/\-_]+={0,2}$")


def try_decode(text: str):
    compact = re.sub(r"\s+", "", text)
    if not compact or not B64_RE.match(compact):
        return None
    has_std = "+" in compact or "/" in compact
    has_url = "-" in compact or "_" in compact
    if has_std and has_url:
        return None
    if has_url:
        compact = compact.replace("-", "+").replace("_", "/")
    compact += "=" * (-len(compact) % 4)
    try:
        raw = base64.b64decode(compact, validate=True)
        decoded = raw.decode("utf-8")
    except (binascii.Error, UnicodeDecodeError):
        return None
    has_control = any(ord(c) < 32 and c not in "\n\t\r" or ord(c) == 127 for c in decoded)
    if not decoded or has_control:
        return None
    return decoded


def main():
    text = sys.stdin.read().strip()
    if not text:
        return
    decoded = try_decode(text)
    if decoded is not None:
        sys.stdout.write(decoded)
        return
    sys.stdout.write(base64.b64encode(text.encode("utf-8")).decode("ascii"))


main()
