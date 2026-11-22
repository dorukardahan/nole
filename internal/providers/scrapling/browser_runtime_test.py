"""Offline contracts; fake protocol events are not real-browser evidence."""
import json
import inspect
import os
import sys
import types
import unittest
from unittest.mock import patch

import browser_runtime as runtime


class Session:
    def __init__(self, fail=None):
        self.calls = []
        self.handlers = {}
        self.fail = fail

    def on(self, event, callback):
        self.handlers[event] = callback

    def send(self, method, params):
        if method == self.fail:
            raise RuntimeError("synthetic secret diagnostic")
        self.calls.append({"method": method, "params": params})

    def emit(self, event, params):
        self.handlers[event](params)


class Context:
    def __init__(self, fail=None):
        self.session = Session(fail)
        self.closed = False
        self.routes = []

    def close(self):
        self.closed = True

    def route(self, pattern, handler):
        def invoke(route):
            # Playwright supplies (route, request), then trims positional args
            # according to the public callback signature. Variadic callbacks
            # receive both, even when their wrapped handler accepts only one.
            parameters = inspect.signature(handler).parameters.values()
            if any(p.kind == inspect.Parameter.VAR_POSITIONAL for p in parameters):
                return handler(route, route.request)
            return handler(route)
        self.routes.append(invoke)

    def route_web_socket(self, pattern, handler):
        self.websocket = handler

    def new_cdp_session(self, page):
        return self.session


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.context = Context()
        self.page = types.SimpleNamespace(context=self.context)
        self.guard = runtime.BrowserGuard(lambda url: url.startswith("https://public.example/"))
        self.guard.setup(self.page)
        self.session = self.context.session

    def unpack(self):
        msg = self.session.calls[-1]
        path = []
        while msg["method"] == "Target.sendMessageToTarget":
            path.append(msg["params"]["sessionId"])
            msg = json.loads(msg["params"]["message"])
        return path, msg

    def deliver(self, path, msg):
        for sid in reversed(path[1:]):
            msg = {"method": "Target.receivedMessageFromTarget", "params": {
                "sessionId": sid, "message": json.dumps(msg)}}
        self.session.emit("Target.receivedMessageFromTarget", {
            "sessionId": path[0], "message": json.dumps(msg)})

    def attach(self, path):
        event = {"sessionId": path[-1], "targetInfo": {"type": "iframe"}, "waitingForDebugger": True}
        if len(path) == 1:
            self.session.emit("Target.attachedToTarget", event)
        else:
            self.deliver(path[:-1], {"method": "Target.attachedToTarget", "params": event})

    def ready(self, path):
        for method in ("Fetch.enable", "Target.setAutoAttach", "Runtime.runIfWaitingForDebugger"):
            actual, msg = self.unpack()
            self.assertEqual((actual, msg["method"]), (path, method))
            self.deliver(path, {"id": msg["id"], "result": {}})

    def test_nested_frame_waits_for_acknowledgements_and_checks_redirect(self):
        self.attach(["outer"])
        self.assertFalse(any(c["method"] == "Runtime.runIfWaitingForDebugger" for c in self.session.calls))
        self.ready(["outer"])
        self.attach(["outer", "inner"])
        self.ready(["outer", "inner"])
        for url, expected in (("http://127.0.0.1/", "Fetch.failRequest"),
                              ("https://public.example/allowed", "Fetch.continueRequest")):
            self.deliver(["outer", "inner"], {"method": "Fetch.requestPaused", "params": {
                "requestId": "r", "request": {"url": url}, "redirectedRequestId": "previous"}})
            path, msg = self.unpack()
            self.assertEqual((path, msg["method"]), (["outer", "inner"], expected))

    def test_main_redirect_is_checked_before_continue(self):
        self.session.emit("Fetch.requestPaused", {"requestId": "r", "request": {
            "url": "http://169.254.169.254/"}, "redirectedRequestId": "previous"})
        self.assertEqual(self.session.calls[-1]["method"], "Fetch.failRequest")

    def test_popup_initial_request_aborted_but_same_page_frame_continues(self):
        for owner, expected in ((self.page, "continue"), (object(), "abort")):
            calls = []
            route = types.SimpleNamespace(request=types.SimpleNamespace(frame=types.SimpleNamespace(page=owner)),
                continue_=lambda: calls.append("continue"), abort=lambda: calls.append("abort"))
            self.context.routes[0](route)
            self.assertEqual(calls, [expected])

    def test_websocket_is_closed_without_connecting(self):
        calls = []
        self.context.websocket(types.SimpleNamespace(close=lambda: calls.append("closed"),
            connect_to_server=lambda: calls.append("connected")))
        self.assertEqual(calls, ["closed"])

    def test_setup_failure_stops_before_navigation_despite_upstream_except_exception(self):
        page = types.SimpleNamespace(context=Context("Target.setAutoAttach"))
        navigated = []
        with self.assertRaises(runtime.BrowserGuardError):
            try:
                runtime.BrowserGuard().setup(page)
            except Exception:
                pass  # Scrapling catches ordinary page_setup exceptions.
            navigated.append(True)
        self.assertTrue(page.context.closed)
        self.assertFalse(navigated)

    def test_child_enable_error_never_resumes_and_discards_payload(self):
        self.attach(["outer"])
        _, msg = self.unpack()
        with self.assertRaises(runtime.BrowserGuardError) as raised:
            self.deliver(["outer"], {"id": msg["id"], "error": {"message": "synthetic password"}})
        self.assertNotIn("password", str(raised.exception))
        self.assertTrue(self.context.closed)
        self.assertEqual(self.unpack()[1]["method"], "Fetch.enable")

    def test_unpaused_new_frame_is_not_resumed(self):
        with self.assertRaises(runtime.BrowserGuardError):
            self.session.emit("Target.attachedToTarget", {"sessionId": "f", "targetInfo": {
                "type": "iframe"}, "waitingForDebugger": False})

    def test_detached_frame_cannot_resume_from_late_ack(self):
        self.attach(["outer"])
        _, msg = self.unpack()
        self.session.emit("Target.detachedFromTarget", {"sessionId": "outer"})
        count = len(self.session.calls)
        self.deliver(["outer"], {"id": msg["id"], "result": {}})
        self.assertEqual(len(self.session.calls), count)


class TargetTests(unittest.TestCase):
    def test_prohibited_literals_do_not_resolve_or_continue(self):
        hosts = ["127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
                 "0.0.0.0", "100.64.0.1", "198.18.0.1", "224.0.0.1", "0177.0.0.1",
                 "0x7f000001", "2130706433", "localhost.", "[::1]", "[fc00::1]",
                 "[fe80::1]", "[::ffff:127.0.0.1]", "[2002:7f00:1::]", "[::a00:1]",
                 "[64:ff9b::a00:1]", "[2001:db8::1]"]
        for host in hosts:
            with self.subTest(host=host), patch.object(runtime.socket, "getaddrinfo", side_effect=AssertionError("DNS used")) as resolve:
                self.assertFalse(runtime.browser_public_url("http://" + host + "/"))
                resolve.assert_not_called()

    def test_dns_all_answers_checked_each_time(self):
        def answer(ip): return (2, 1, 6, "", (ip, 443))
        with patch.object(runtime.socket, "getaddrinfo", side_effect=[
            [answer("8.8.8.8")], [answer("8.8.8.8"), answer("10.0.0.1")], []]):
            self.assertTrue(runtime.browser_public_url("https://public.example/"))
            self.assertFalse(runtime.browser_public_url("https://public.example/"))
            self.assertFalse(runtime.browser_public_url("https://public.example/"))

    def test_public_literals_and_supported_schemes(self):
        self.assertTrue(runtime.browser_public_url("https://8.8.8.8/short"))
        self.assertTrue(runtime.browser_public_url("https://[2606:4700:4700::1111]/"))
        self.assertFalse(runtime.browser_public_url("file:///tmp/example"))


class FetchTests(unittest.TestCase):
    def test_readiness_launches_guards_without_navigation_and_closes(self):
        calls = []
        context = Context()
        context.new_page = lambda: types.SimpleNamespace(context=context)
        class LocalSession:
            def __init__(self, **options):
                calls.append(options)
                self.context = context
            def __enter__(self): return self
            def __exit__(self, *args): context.close()
        fetchers = types.ModuleType("scrapling.fetchers")
        fetchers.StealthySession = LocalSession
        with patch.dict(sys.modules, {"scrapling.fetchers": fetchers}):
            runtime.check_browser("fixture-profile")
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0]["user_data_dir"], "fixture-profile")
        self.assertTrue(context.closed)
        self.assertEqual(context.session.calls[0]["method"], "Fetch.enable")

    def test_browser_uses_solver_one_attempt_and_private_proxy_dictionary(self):
        calls = []
        page = object()
        fetchers = types.ModuleType("scrapling.fetchers")
        fetchers.StealthyFetcher = types.SimpleNamespace(fetch=lambda url, **kw: (calls.append((url, kw)) or page))
        with patch.dict(sys.modules, {"scrapling.fetchers": fetchers}), patch.object(runtime, "browser_public_url", return_value=True):
            result = runtime.fetch_browser("https://public.example/", timeout_ms=60000,
                proxy="http://user%40test:pass%3Aword@proxy.example:8080", profile="fixture-profile")
        self.assertIs(result, page)
        self.assertEqual(len(calls), 1)
        opts = calls[0][1]
        self.assertTrue(opts["solve_cloudflare"])
        self.assertEqual((opts["timeout"], opts["retries"]), (60000, 1))
        self.assertEqual(opts["proxy"], {"server": "http://proxy.example:8080", "username": "user@test", "password": "pass:word"})
        self.assertEqual(opts["additional_args"]["service_workers"], "block")
        self.assertFalse(opts["additional_args"]["ignore_https_errors"])
        self.assertEqual(opts["user_data_dir"], "fixture-profile")

    def test_incompatible_proxy_never_launches_or_falls_back(self):
        for proxy in ("socks5h://proxy.example:1080", "socks4a://proxy.example:1080", "socks5://user:secret@proxy.example:1080"):
            with self.subTest(proxy=proxy), self.assertRaises(runtime.BrowserProxyError) as raised:
                runtime.fetch_browser("https://8.8.8.8/", proxy=proxy)
            self.assertNotIn("secret", str(raised.exception))

    def test_runtime_failure_has_no_raw_diagnostic_or_retry(self):
        fetchers = types.ModuleType("scrapling.fetchers")
        fetchers.StealthyFetcher = types.SimpleNamespace(fetch=unittest.mock.Mock(side_effect=RuntimeError("private target password")))
        with patch.dict(sys.modules, {"scrapling.fetchers": fetchers}), self.assertRaises(runtime.BrowserFetchError) as raised:
            runtime.fetch_browser("https://8.8.8.8/")
        self.assertNotIn("password", str(raised.exception))
        self.assertEqual(fetchers.StealthyFetcher.fetch.call_count, 1)

    def test_missing_browser_module_is_fixed_unavailability(self):
        with patch.dict(sys.modules, {"scrapling.fetchers": None}), self.assertRaises(runtime.BrowserUnavailable) as raised:
            runtime.fetch_browser("https://8.8.8.8/")
        self.assertEqual(str(raised.exception), "browser_unavailable")

    def test_default_profile_is_isolated_and_removed_after_failure(self):
        profiles = []
        def fail(url, **options):
            profiles.append(options["user_data_dir"])
            self.assertTrue(os.path.isdir(profiles[-1]))
            raise RuntimeError("fixture failure")
        fetchers = types.ModuleType("scrapling.fetchers")
        fetchers.StealthyFetcher = types.SimpleNamespace(fetch=fail)
        with patch.dict(sys.modules, {"scrapling.fetchers": fetchers}):
            for _ in range(2):
                with self.assertRaises(runtime.BrowserFetchError):
                    runtime.fetch_browser("https://8.8.8.8/")
        self.assertNotEqual(profiles[0], profiles[1])
        self.assertTrue(all(not os.path.exists(p) for p in profiles))


if __name__ == "__main__":
    unittest.main()
