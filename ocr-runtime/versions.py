#!/usr/bin/env python3
"""Report exact OCR runtime package and executable versions without input data."""

import importlib.metadata
import json
import os
import platform
import subprocess


def executable_version(argv):
    try:
        result = subprocess.run(
            argv, check=False, capture_output=True, text=True, timeout=10
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        return f"unavailable: {exc.__class__.__name__}"
    output = (result.stdout or result.stderr).strip().splitlines()
    return output[0] if output else f"exit={result.returncode}"


packages = {}
for distribution in (
    "ocrmypdf",
    "ocrmypdf-paddleocr",
    "rapidocr",
    "onnxruntime",
    "paddlepaddle",
    "paddleocr",
    "paddlex",
):
    try:
        packages[distribution] = importlib.metadata.version(distribution)
    except importlib.metadata.PackageNotFoundError:
        packages[distribution] = None

report = {
    "application": {
        "version": os.getenv("APP_VERSION", "unknown"),
        "commit": os.getenv("APP_COMMIT", "unknown"),
        "build_date": os.getenv("APP_BUILD_DATE", "unknown"),
    },
    "ocr_plugin_commit": os.getenv("OCR_PADDLE_PLUGIN_COMMIT", "unknown"),
    "python": platform.python_version(),
    "packages": packages,
    "executables": {
        "ocrmypdf": executable_version(["/opt/ocr/bin/ocrmypdf", "--version"]),
        "ghostscript": executable_version(["/usr/bin/gs", "--version"]),
        "qpdf": executable_version(["/usr/bin/qpdf", "--version"]),
        "pdfinfo": executable_version(["/usr/bin/pdfinfo", "-v"]),
        "pdftotext": executable_version(["/usr/bin/pdftotext", "-v"]),
    },
}
print(json.dumps(report, indent=2, sort_keys=True))
