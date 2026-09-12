"""Environment-driven configuration for the paperless-gpt parser plugin."""

from __future__ import annotations

import os
from dataclasses import dataclass


def _truthy(value: str | None, default: bool) -> bool:
    if value is None:
        return default
    return value.strip().lower() in {"1", "true", "yes", "on"}


@dataclass(frozen=True, slots=True)
class Config:
    """Runtime configuration read from environment variables.

    Read once at module load. The shim is short-lived per Celery worker so we
    do not need to support live reloading.
    """

    base_url: str
    api_token: str | None
    enabled: bool
    score: int | None
    timeout_seconds: float

    @classmethod
    def from_env(cls) -> "Config":
        return cls(
            base_url=os.getenv("PAPERLESS_GPT_URL", "http://paperless-gpt:8080").rstrip("/"),
            api_token=os.getenv("PAPERLESS_GPT_API_TOKEN") or None,
            enabled=_truthy(os.getenv("PAPERLESS_GPT_PARSER_ENABLED"), default=True),
            score=int(os.environ["PAPERLESS_GPT_PARSER_SCORE"])
            if os.getenv("PAPERLESS_GPT_PARSER_SCORE")
            else None,
            timeout_seconds=float(os.getenv("PAPERLESS_GPT_PARSER_TIMEOUT", "300")),
        )
