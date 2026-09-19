"""GptParser — implements paperless-ngx 3.0 ParserProtocol.

Discovered by paperless-ngx via the ``paperless_ngx.parsers`` entrypoint group;
forwards each document to a paperless-gpt sidecar over HTTP.

Note: this module imports nothing from paperless-ngx. The protocol is
structural (typing.Protocol with @runtime_checkable upstream), so the registry
validates compatibility at import time by attribute presence.
"""

from __future__ import annotations

import datetime
import logging
import shutil
import tempfile
from pathlib import Path
from types import TracebackType
from typing import Any, Self

from PIL import Image, ImageDraw

from .client import Capabilities, GptClient, ParseResult
from .config import Config

logger = logging.getLogger("paperless_gpt_parser")

# Lazily resolved on first call to supported_mime_types() / score(). The
# sidecar may not be reachable at import time (e.g. paperless-ngx starts
# faster than paperless-gpt during a docker-compose up), so we never raise
# from class-level code.
_capabilities_cache: Capabilities | None = None
_capabilities_failed = False


def _get_capabilities() -> Capabilities | None:
    global _capabilities_cache, _capabilities_failed
    if _capabilities_cache is not None:
        return _capabilities_cache
    if _capabilities_failed:
        return None
    cfg = Config.from_env()
    if not cfg.enabled:
        return None
    try:
        with GptClient(cfg) as c:
            _capabilities_cache = c.capabilities()
            return _capabilities_cache
    except Exception as exc:  # pragma: no cover — network errors at runtime
        logger.warning("paperless-gpt /capabilities unreachable: %s", exc)
        _capabilities_failed = True
        return None


class GptParser:
    """ParserProtocol implementation backed by a paperless-gpt HTTP sidecar."""

    name = "paperless-gpt"
    version = "0.1.0a1"
    author = "icereed"
    url = "https://github.com/icereed/paperless-gpt"

    # ------------------------------------------------------------------
    # Class-level discovery (called by ParserRegistry)
    # ------------------------------------------------------------------

    @classmethod
    def supported_mime_types(cls) -> dict[str, str]:
        caps = _get_capabilities()
        if caps is None:
            return {}
        return dict(caps.supported_mime_types)

    @classmethod
    def score(
        cls,
        mime_type: str,
        filename: str,  # noqa: ARG003 — part of the protocol
        path: Path | None = None,  # noqa: ARG003
    ) -> int | None:
        caps = _get_capabilities()
        if caps is None:
            return None
        if mime_type not in caps.supported_mime_types:
            return None
        cfg = Config.from_env()
        return cfg.score if cfg.score is not None else caps.default_score

    # ------------------------------------------------------------------
    # Properties
    # ------------------------------------------------------------------

    @property
    def can_produce_archive(self) -> bool:
        caps = _get_capabilities()
        return bool(caps and caps.can_produce_archive)

    @property
    def requires_pdf_rendition(self) -> bool:
        caps = _get_capabilities()
        return bool(caps and caps.requires_pdf_rendition)

    # ------------------------------------------------------------------
    # Lifecycle
    # ------------------------------------------------------------------

    def __init__(self) -> None:
        self._tempdir: Path | None = None
        self._result: ParseResult | None = None
        self._archive_path: Path | None = None
        self._mailrule_id: int | None = None

    def configure(self, context: Any) -> None:
        # ParserContext is a frozen dataclass with `mailrule_id` today;
        # forward it through the opaque `context` field on /parse.
        self._mailrule_id = getattr(context, "mailrule_id", None)

    def __enter__(self) -> Self:
        self._tempdir = Path(tempfile.mkdtemp(prefix="paperless-gpt-"))
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc_val: BaseException | None,
        exc_tb: TracebackType | None,
    ) -> None:
        if self._tempdir and self._tempdir.exists():
            shutil.rmtree(self._tempdir, ignore_errors=True)
        self._tempdir = None
        self._result = None
        self._archive_path = None

    # ------------------------------------------------------------------
    # Core parse
    # ------------------------------------------------------------------

    def parse(
        self,
        document_path: Path,
        mime_type: str,
        *,
        produce_archive: bool = True,
    ) -> None:
        ctx: dict[str, Any] = {"source": "paperless-ngx"}
        if self._mailrule_id is not None:
            ctx["mailrule_id"] = self._mailrule_id

        with GptClient() as client:
            self._result = client.parse(
                document_path,
                mime_type,
                produce_archive=produce_archive,
                produce_thumbnail=True,
                context=ctx,
            )

        if self._result.archive_pdf and self._tempdir:
            self._archive_path = self._tempdir / "archive.pdf"
            self._archive_path.write_bytes(self._result.archive_pdf)

    # ------------------------------------------------------------------
    # Result accessors
    # ------------------------------------------------------------------

    def get_text(self) -> str | None:
        return self._result.text if self._result else None

    def get_date(self) -> datetime.datetime | None:
        # The sidecar exposes `date` as ISO-8601 once implemented; for now
        # the MVP doesn't fill it in.
        return None

    def get_archive_path(self) -> Path | None:
        return self._archive_path

    def get_thumbnail(self, document_path: Path, mime_type: str) -> Path:
        if self._tempdir is None:
            self._tempdir = Path(tempfile.mkdtemp(prefix="paperless-gpt-"))
        out = self._tempdir / "thumb.webp"
        if self._result and self._result.thumbnail_webp:
            out.write_bytes(self._result.thumbnail_webp)
            return out
        # Fallback so paperless-ngx never sees a missing path: render a tiny
        # placeholder. Real thumbnail will come from the sidecar in a follow-up.
        img = Image.new("RGB", (200, 280), color="white")
        draw = ImageDraw.Draw(img)
        draw.text((8, 8), "paperless-gpt", fill="black")
        img.save(out, format="WEBP")
        return out

    def get_page_count(self, document_path: Path, mime_type: str) -> int | None:
        return self._result.page_count if self._result else None

    def extract_metadata(
        self,
        document_path: Path,
        mime_type: str,
    ) -> list[dict[str, str]]:
        # The sidecar will expose this once we have a story for per-format
        # metadata extraction. Returning [] is the spec-defined no-op.
        return []
