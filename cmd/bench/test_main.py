import importlib.util
import json
import pathlib
import unittest
from unittest import mock

MODULE_PATH = pathlib.Path(__file__).with_name("main.py")
spec = importlib.util.spec_from_file_location("bench_main", MODULE_PATH)
bench_main = importlib.util.module_from_spec(spec)
spec.loader.exec_module(bench_main)


class FakeHTTPResponse:
    def __init__(self, payload):
        self.payload = payload

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        return False

    def read(self):
        return json.dumps(self.payload).encode("utf-8")


def request_header(req, name):
    return dict(req.header_items()).get(name)


class BenchmarkSecretHandlingTest(unittest.TestCase):
    def assert_provider_uses_urlopen_without_subprocess(self, call, payload):
        captured = {}

        def fake_urlopen(req, timeout):
            captured["request"] = req
            captured["timeout"] = timeout
            return FakeHTTPResponse(payload)

        with mock.patch.object(bench_main.subprocess, "run") as run_mock, \
             mock.patch.object(bench_main.urllib.request, "urlopen", side_effect=fake_urlopen):
            result = call()

        run_mock.assert_not_called()
        self.assertEqual(result["result_count"], 1)
        self.assertEqual(captured["timeout"], 3)
        return captured["request"]

    def test_tavily_api_key_is_not_passed_in_process_argv(self):
        req = self.assert_provider_uses_urlopen_without_subprocess(
            lambda: bench_main.run_tavily(
                "nole research",
                {"TAVILY_API_KEY": "tavily-secret-value"},
                timeout=3,
            ),
            {
                "results": [
                    {"title": "Nólë", "url": "https://example.com", "content": "deep research"}
                ]
            },
        )

        self.assertEqual(req.full_url, "https://api.tavily.com/search")
        self.assertEqual(req.method, "POST")
        self.assertIn(b"tavily-secret-value", req.data)

    def test_brave_api_key_is_not_passed_in_process_argv(self):
        req = self.assert_provider_uses_urlopen_without_subprocess(
            lambda: bench_main.run_brave(
                "nole research",
                {"BRAVE_SEARCH_API_KEY": "brave-secret-value"},
                timeout=3,
            ),
            {
                "web": {
                    "results": [
                        {"title": "Nólë", "url": "https://example.com", "description": "deep research"}
                    ]
                }
            },
        )

        self.assertEqual(req.full_url, "https://api.search.brave.com/res/v1/web/search?q=nole%20research&count=5")
        self.assertEqual(req.method, "GET")
        self.assertEqual(request_header(req, "X-subscription-token"), "brave-secret-value")

    def test_firecrawl_api_key_is_not_passed_in_process_argv(self):
        req = self.assert_provider_uses_urlopen_without_subprocess(
            lambda: bench_main.run_firecrawl_search(
                "nole research",
                {"FIRECRAWL_API_KEY": "firecrawl-secret-value"},
                timeout=3,
            ),
            {
                "data": {
                    "web": [
                        {"title": "Nólë", "url": "https://example.com", "description": "deep research"}
                    ]
                }
            },
        )

        self.assertEqual(req.full_url, "https://api.firecrawl.dev/v2/search")
        self.assertEqual(req.method, "POST")
        self.assertEqual(request_header(req, "Authorization"), "Bearer firecrawl-secret-value")
        self.assertNotIn(b"firecrawl-secret-value", req.data or b"")


if __name__ == "__main__":
    unittest.main()
