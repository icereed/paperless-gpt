"""HTTP client for the paperless-gpt /api/v1 surface."""

from __future__ import annotations

import base64
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import httpx

from .config import Config


@dataclass(slots=True)
class Capabilities:
    """Subset of /capabilities the shim cares about."""

    name: str
    version: str
    supported_mime_types: dict[str, str]
    can_produce_archive: bool
    requires_pdf_rendition: bool
    default_score: int


@dataclass(slots=True)
class ParseResult:
    """Subset of /parse the shim cares about."""

    text: str
    page_count: int | None
    archive_pdf: bytes | None
    thumbnail_webp: bytes | None
    provider: str | None


class GptClient:
    """Thin httpx wrapper. Stateless; one instance per parser invocation."""

    def __init__(self, config: Config | None = None) -> None:
        self._config = config or Config.from_env()
        headers: dict[str, str] = {}
        if self._config.api_token:
            headers["Authorization"] = f"Bearer {self._config.api_token}"
        self._client = httpx.Client(
            base_url=self._config.base_url,
            timeout=self._config.timeout_seconds,
            headers=headers,
        )

    def close(self) -> None:
        self._client.close()

    def __enter__(self) -> "GptClient":
        return self

    def __exit__(self, *_: Any) -> None:
        self.close()

    # ------------------------------------------------------------------
    # API calls
    # ------------------------------------------------------------------

    def capabilities(self) -> Capabilities:
        resp = self._client.get("/api/v1/capabilities")
        resp.raise_for_status()
        body = resp.json()
        return Capabilities(
            name=body.get("name", "paperless-gpt"),
            version=body.get("version", "unknown"),
            supported_mime_types=dict(body.get("supported_mime_types") or {}),
            can_produce_archive=bool(body.get("can_produce_archive", False)),
            requires_pdf_rendition=bool(body.get("requires_pdf_rendition", False)),
            default_score=int(body.get("default_score", 50)),
        )

    def parse(
        self,
        document_path: Path,
        mime_type: str,
        *,
        produce_archive: bool = True,
        produce_thumbnail: bool = True,
        context: dict[str, Any] | None = None,
    ) -> ParseResult:
        files = {"file": (document_path.name, document_path.open("rb"), mime_type)}
        data: dict[str, str] = {
            "mime_type": mime_type,
            "filename": document_path.name,
            "produce_archive": "true" if produce_archive else "false",
            "produce_thumbnail": "true" if produce_thumbnail else "false",
        }
        if context is not None:
            import json

            data["context"] = json.dumps(context)

        try:
            resp = self._client.post("/api/v1/parse", data=data, files=files)
        finally:
            files["file"][1].close()

        resp.raise_for_status()
        body = resp.json()
        return ParseResult(
            text=body.get("text", "") or "",
            page_count=body.get("page_count"),
            archive_pdf=_b64(body.get("archive_pdf_b64")),
            thumbnail_webp=_b64(body.get("thumbnail_webp_b64")),
            provider=body.get("provider"),
        )


def _b64(value: str | None) -> bytes | None:
    if not value:
        return None
    return base64.b64decode(value)
