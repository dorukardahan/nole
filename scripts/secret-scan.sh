#!/usr/bin/env bash
set -euo pipefail

# Public-safety scan for tracked text files. This is intentionally conservative:
# it fails on real-looking secrets, private key blocks, auth headers, and
# personal machine paths while allowing documented environment variable names and
# obvious placeholders.

python3 - <<'PY'
from __future__ import annotations

import json
import pathlib
import re
import subprocess
import sys

SAFE_VALUE_RE = re.compile(
    r"^(?:$|\.\.\.|\*\*\*|<[^>]+>|\$\{?[A-Z0-9_]+\}?|replace-with-[a-z0-9-]+|your-[a-z0-9-]+|example(?:-[a-z0-9-]+)?|set-locally|REDACTED)$",
    re.IGNORECASE,
)

ASSIGNMENT_RE = re.compile(
    r"(?i)(?:^|\b|export\s+)([A-Z0-9_]*(?:API[_-]?KEY|TOKEN|SECRET|PASSWORD|PASSWD|AUTHORIZATION|BEARER)[A-Z0-9_]*)\s*[:=]\s*([\"']?)([^\"'\s#`]+)\2"
)
PRIVATE_KEY_RE = re.compile(r"-----BEGIN (?:RSA |OPENSSH |EC |DSA )?PRIVATE KEY-----")
AUTH_HEADER_RE = re.compile(r"(?i)" + "Author" + r"ization:\s*(?:" + "Bearer" + r"|token)\s+([^\s`]+)")
PRIVATE_PATH_RE = re.compile(r"/(?:home|Users|opt)/(?!USER\b|user\b|example\b|path\b|absolute\b)[A-Za-z0-9._-]+")
RAW_ENV_RE = re.compile(r"(?i)^\s*[A-Z0-9_]*(?:API[_-]?KEY|TOKEN|SECRET|PASSWORD|PASSWD)\s*=")


def tracked_files() -> list[str]:
    return subprocess.check_output(["git", "ls-files"], text=True).splitlines()


def is_text(path: pathlib.Path) -> bool:
    try:
        data = path.read_bytes()
    except OSError:
        return False
    if b"\0" in data:
        return False
    try:
        data.decode("utf-8")
    except UnicodeDecodeError:
        return False
    return True


def safe_value(value: str) -> bool:
    value = value.strip().strip('"\'')
    if SAFE_VALUE_RE.match(value):
        return True
    if re.fullmatch(r"(?:SECRET|TEST|FAKE|DUMMY|EXAMPLE)[A-Z0-9_-]*", value, re.IGNORECASE):
        return True
    if len(value) < 12:
        return True
    return False


findings: list[dict[str, object]] = []
for file_name in tracked_files():
    path = pathlib.Path(file_name)
    if not path.exists() or path.is_dir() or not is_text(path):
        continue
    text = path.read_text(encoding="utf-8", errors="replace")
    for line_no, line in enumerate(text.splitlines(), 1):
        stripped = line.strip()
        if PRIVATE_KEY_RE.search(line):
            findings.append({"file": file_name, "line": line_no, "kind": "private_key", "text": "private key block marker"})
        m = AUTH_HEADER_RE.search(line)
        if m and not safe_value(m.group(1)):
            findings.append({"file": file_name, "line": line_no, "kind": "auth_header", "text": "Authorization header with credential-like value"})
        for assignment in ASSIGNMENT_RE.finditer(line):
            value = assignment.group(3)
            if not safe_value(value):
                findings.append({"file": file_name, "line": line_no, "kind": "secret_assignment", "text": f"{assignment.group(1)}=[REDACTED]"})
        if RAW_ENV_RE.search(line) and not any(marker in stripped for marker in ("...", "***", "<", "replace-with", "your-")):
            # RAW_ENV_RE mostly catches dotenv examples. The assignment check above
            # decides whether a value is actually suspicious; keep this as an
            # extra hint only when the line is not a documented placeholder.
            m = ASSIGNMENT_RE.search(line)
            if m and not safe_value(m.group(3)):
                findings.append({"file": file_name, "line": line_no, "kind": "dotenv_shape", "text": f"{m.group(1)}=[REDACTED]"})
        if PRIVATE_PATH_RE.search(line):
            findings.append({"file": file_name, "line": line_no, "kind": "private_path", "text": PRIVATE_PATH_RE.sub("/[REDACTED-PATH]", stripped)[:200]})

if findings:
    print(json.dumps({"ok": False, "findings": findings}, indent=2, ensure_ascii=False))
    sys.exit(1)

print(json.dumps({"ok": True, "files_scanned": len(tracked_files())}, indent=2))
PY
