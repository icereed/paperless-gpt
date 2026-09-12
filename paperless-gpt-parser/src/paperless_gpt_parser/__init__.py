"""paperless-gpt parser plugin for paperless-ngx 3.0.

Forwards documents to a running paperless-gpt sidecar over HTTP.
See README.md and the upstream RFC at
https://github.com/icereed/paperless-gpt/pull/964.
"""

from .parser import GptParser

__all__ = ["GptParser"]
__version__ = "0.1.0a1"
