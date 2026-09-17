#!/usr/bin/env python3
"""Sync selected keys from repo-root .env into k8s secrets.local.yaml (no stdout of secrets)."""
from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ENV_PATH = ROOT / ".env"
SECRETS_PATH = ROOT / "k8s/manifests/config/secrets.local.yaml"

SYNC = [
    "DB_PASSWORD",
    "SESSION_SECRET",
    "TOKEN_ENCRYPTION_KEY",
    "WEBHOOK_SECRET",
    "DISCORD_CLIENT_ID",
    "DISCORD_CLIENT_SECRET",
    "DISCORD_BOT_TOKEN",
    "GOOGLE_CLIENT_ID",
    "GOOGLE_CLIENT_SECRET",
    "GOOGLE_PUBSUB_TOPIC",
    "CF_TUNNEL_TOKEN",
]

OPTIONAL_SYNC = [
    "SMTP_URL",
    "SMTP_HOST",
    "SMTP_PORT",
    "SMTP_USER",
    "SMTP_PASS",
    "SMTP_SECURE",
    "SMTP_FROM",
]


def parse_env(path: Path) -> dict[str, str]:
    out: dict[str, str] = {}
    if not path.is_file():
        raise SystemExit(f"missing {path}")
    for line in path.read_text().splitlines():
        s = line.strip()
        if not s or s.startswith("#") or "=" not in s:
            continue
        k, _, v = s.partition("=")
        k = k.strip()
        v = v.strip().strip('"').strip("'")
        out[k] = v
    return out


def yaml_quote(value: str) -> str:
    escaped = value.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def set_secret_key(text: str, key: str, value: str) -> str:
    pattern = re.compile(rf"(?m)^  {re.escape(key)}:\s*.*$")
    line = f"  {key}: {yaml_quote(value)}"
    if pattern.search(text):
        return pattern.sub(line, text, count=1)
    if "stringData:" not in text:
        raise SystemExit("secrets.local.yaml missing stringData")
    return text.replace("stringData:\n", f"stringData:\n{line}\n", 1)


def main() -> None:
    env = parse_env(ENV_PATH)
    if not SECRETS_PATH.is_file():
        raise SystemExit(f"missing {SECRETS_PATH} - copy from secrets.local.yaml.example first")

    text = SECRETS_PATH.read_text()
    missing = []
    for key in SYNC:
        val = env.get(key, "").strip()
        if not val:
            missing.append(key)
            continue
        text = set_secret_key(text, key, val)

    db_pw = env.get("DB_PASSWORD", "").strip()
    if db_pw:
        url = f"postgres://postgres:{db_pw}@postgres:5432/sift?sslmode=disable"
        text = set_secret_key(text, "DATABASE_URL", url)
        text = set_secret_key(text, "DB_PASSWORD", db_pw)

    SECRETS_PATH.write_text(text)
    print(f"Synced {len(SYNC) - len(missing)} required keys into secrets.local.yaml")
    optional_n = 0
    for key in OPTIONAL_SYNC:
        val = env.get(key, "").strip()
        if not val:
            continue
        text = set_secret_key(text, key, val)
        optional_n += 1
    if optional_n:
        SECRETS_PATH.write_text(text)
        print(f"Synced {optional_n} optional SMTP/mail keys")
    if missing:
        print("Skipped empty/missing .env keys:", ", ".join(missing), file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
