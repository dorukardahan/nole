"""Offline contracts; real Chromium coverage lives in browser_integration_test.go."""
import types
import sys
import unittest
from unittest.mock import patch
import browser_runtime as runtime

PROXY = 'http://nole:synthetic-token@127.0.0.1:12345'
ENDPOINT = 'http://127.0.0.1:23456'

class Context:
    def __init__(self): self.closed = False
    def route(self, pattern, callback): self.handler = callback
    def route_web_socket(self, pattern, callback): self.websocket = callback
    def close(self): self.closed = True
    def new_page(self): return types.SimpleNamespace(context=self)

class GuardTests(unittest.TestCase):
    def setUp(self):
        self.context = Context()
        self.guard = runtime.BrowserGuard()
        self.guard.install(self.context)
        self.page = self.context.new_page()

    def route(self, owner, error=None):
        calls = []
        def proceed():
            if error: raise RuntimeError(error)
            calls.append('continue')
        route = types.SimpleNamespace(request=types.SimpleNamespace(frame=types.SimpleNamespace(page=owner)),
            continue_=proceed, abort=lambda: calls.append('abort'))
        self.context.handler(route)
        return calls

    def test_missing_setup_blocks_before_network(self):
        self.assertEqual(self.route(self.page), ['abort'])
        self.assertFalse(self.guard.installed)

    def test_setup_opens_only_owned_page(self):
        self.guard.setup(self.page)
        self.assertEqual(self.route(self.page), ['continue'])
        self.assertEqual(self.route(object()), ['abort'])

    def test_setup_on_wrong_context_fails_closed(self):
        with self.assertRaises(runtime.BrowserGuardError):
            self.guard.setup(Context().new_page())
        self.assertTrue(self.context.closed)

    def test_request_cancellation_does_not_close_context(self):
        self.guard.setup(self.page)
        self.route(self.page, 'Request was cancelled')
        self.assertFalse(self.context.closed)
        with self.assertRaises(runtime.BrowserGuardError):
            self.route(self.page, 'unexpected protocol failure with private details')
        self.assertTrue(self.context.closed)

    def test_websocket_is_closed_without_connection(self):
        calls = []
        self.context.websocket(types.SimpleNamespace(close=lambda: calls.append('closed')))
        self.assertEqual(calls,['closed'])

class FetchTests(unittest.TestCase):
    def session_module(self, setup=True, fail=False):
        self.options = []
        self.context = Context()
        owner = self
        class RemoteSession:
            def __init__(self, **options):
                owner.options.append(options)
                self.options = options
                self._context_options = options["additional_args"]
                self.context = owner.context
            def __enter__(self): return self
            def __exit__(self, *args): self.context.close()
            def fetch(self, url):
                if setup: self.options['page_setup'](self.context.new_page())
                if fail: raise RuntimeError('synthetic private diagnostic')
                if url == 'http://nole-readiness.invalid/': return types.SimpleNamespace(status=200,text='nole browser proxy ready')
                return object()
        return types.SimpleNamespace(RemoteSession=RemoteSession)

    def fetch(self, **kw):
        return runtime.fetch_browser('https://public.example/', proxy=PROXY, cdp_url=ENDPOINT, **kw)

    def test_ignored_setup_cannot_succeed(self):
        with patch.dict(sys.modules, {'scrapling.fetchers':self.session_module(setup=False)}):
            with self.assertRaises(runtime.BrowserGuardError): self.fetch()
        self.assertTrue(self.context.closed)

    def test_solver_defaults_off_and_requires_explicit_opt_in(self):
        for enabled in (False, True):
            with patch.dict(sys.modules, {'scrapling.fetchers':self.session_module()}):
                self.fetch(solve_cloudflare=enabled)
            opts = self.options[0]
            self.assertEqual(opts['solve_cloudflare'],enabled)
            self.assertEqual(opts['retries'],1)
            self.assertTrue(opts['new_context'])
            self.assertEqual(opts['proxy']['bypass'],'<-loopback>')
            self.assertEqual(opts['additional_args']['service_workers'],'block')
            self.assertFalse(opts['additional_args']['ignore_https_errors'])
            self.assertEqual(opts['additional_args']['proxy']['bypass'],'<-loopback>')

    def test_runtime_dropping_bypass_cannot_fetch(self):
        module = self.session_module()
        original = module.RemoteSession.__init__
        def initialize(session, **options):
            original(session, **options)
            session._context_options['proxy'].pop('bypass')
        module.RemoteSession.__init__ = initialize
        module.RemoteSession.fetch = lambda *args: self.fail('navigation occurred with missing bypass policy')
        with patch.dict(sys.modules, {'scrapling.fetchers':module}):
            with self.assertRaises(runtime.BrowserGuardError): self.fetch()
        self.assertTrue(self.context.closed)

    def test_missing_proxy_or_owned_endpoint_cannot_launch_fallback(self):
        for args in ({}, {'proxy':PROXY}, {'cdp_url':ENDPOINT}):
            with self.assertRaises(runtime.BrowserGuardError): runtime.fetch_browser('https://public.example/', **args)

    def test_readiness_requires_authenticated_proxy_response(self):
        module = self.session_module()
        module.RemoteSession.fetch = lambda self, url: types.SimpleNamespace(status=407,text='denied')
        with patch.dict(sys.modules, {'scrapling.fetchers':module}):
            with self.assertRaises(runtime.BrowserGuardError):
                runtime.check_browser('unused',proxy=PROXY,cdp_url=ENDPOINT)

    def test_readiness_installs_guard_before_new_page(self):
        with patch.dict(sys.modules, {'scrapling.fetchers':self.session_module()}):
            runtime.check_browser('unused',proxy=PROXY,cdp_url=ENDPOINT)
        self.assertTrue(self.context.closed)
        self.assertTrue(hasattr(self.context,'handler'))
        self.assertFalse(self.options[0]['solve_cloudflare'])

    def test_runtime_errors_are_payload_free(self):
        with patch.dict(sys.modules, {'scrapling.fetchers':self.session_module(fail=True)}):
            with self.assertRaises(runtime.BrowserFetchError) as raised: self.fetch()
        self.assertEqual(str(raised.exception),'browser_fetch_failed')

    def test_unsupported_runtime_fails_without_launch_fallback(self):
        with patch.dict(sys.modules, {'scrapling.fetchers':types.SimpleNamespace()}):
            with self.assertRaises(runtime.BrowserUnavailable): self.fetch()

if __name__ == '__main__': unittest.main()
