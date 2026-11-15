"""Regression tests for Paddle plugin registration, selection, and eng+hin."""

import importlib.metadata
from types import SimpleNamespace
import unittest

import ocrmypdf_paddleocr
from ocrmypdf._plugin_manager import get_plugin_manager
from ocrmypdf.builtin_plugins.rapidocr_ocr import RapidOcrEngine
from ocrmypdf_paddleocr.plugin import PaddleOCREngine


class PaddlePluginSelectionTests(unittest.TestCase):
    def test_package_root_exports_hooks_without_automatic_entrypoint(self):
        self.assertTrue(callable(ocrmypdf_paddleocr.get_ocr_engine))
        paddle_entrypoints = [
            entry
            for entry in importlib.metadata.entry_points(group="ocrmypdf")
            if entry.name == "paddleocr"
        ]
        self.assertEqual(paddle_entrypoints, [])

    def test_explicit_package_plugin_selects_paddle_for_auto(self):
        manager = get_plugin_manager(plugins=["ocrmypdf_paddleocr"])
        engine = manager.get_ocr_engine(
            options=SimpleNamespace(ocr_engine="auto")
        )
        self.assertIsInstance(engine, PaddleOCREngine)

    def test_rapidocr_selection_uses_builtin_engine(self):
        manager = get_plugin_manager(plugins=[])
        engine = manager.get_ocr_engine(
            options=SimpleNamespace(ocr_engine="rapidocr")
        )
        self.assertIsInstance(engine, RapidOcrEngine)

    def test_paddle_hook_declines_rapidocr(self):
        engine = ocrmypdf_paddleocr.get_ocr_engine(
            options=SimpleNamespace(ocr_engine="rapidocr")
        )
        self.assertIsNone(engine)


class EnglishHindiLanguageTests(unittest.TestCase):
    def test_pair_selects_model_supporting_both_languages(self):
        options = SimpleNamespace(languages=["eng", "hin"])
        self.assertEqual(PaddleOCREngine._get_paddle_lang(options), "hi")

    def test_language_validation_accepts_requested_codes(self):
        supported = PaddleOCREngine.languages(None)
        self.assertIn("eng", supported)
        self.assertIn("hin", supported)


if __name__ == "__main__":
    unittest.main()
