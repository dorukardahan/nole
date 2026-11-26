"""Optional Scrapling browser support, embedded into the extraction subprocess.

The Go caller owns Chromium, deadlines, cleanup, and a mandatory local proxy
that validates and pins destination IPs for every browser connection.
"""
import json
import sys
from urllib.parse import unquote, urlsplit


class BrowserUnavailable(Exception):
    def __init__(self):
        super().__init__("browser_unavailable")


class BrowserFetchError(Exception):
    def __init__(self):
        super().__init__("browser_fetch_failed")


class BrowserProxyError(Exception):
    def __init__(self):
        super().__init__("browser_proxy_unsupported")


class BrowserGuardError(BaseException):
    # Scrapling catches Exception from page_setup and continues to goto. A guard
    # failure must escape that handler; its context is also closed before raising.
    def __init__(self):
        super().__init__("browser_guard_failed")


def browser_proxy(proxy):
    """Accept only the private guard endpoint supplied by the Go owner."""
    try:
        parsed = urlsplit(proxy or "")
        if (parsed.scheme != "http" or parsed.hostname != "127.0.0.1"
                or parsed.username != "nole" or not parsed.password or not parsed.port
                or parsed.path not in ("", "/") or parsed.query or parsed.fragment):
            raise ValueError()
        return {"server": "http://127.0.0.1:" + str(parsed.port),
                "username": "nole", "password": unquote(parsed.password)}
    except Exception:
        raise BrowserProxyError() from None


class BrowserGuard:
    """Navigation gate installed on the context before Scrapling can fetch.

    The Go-owned proxy enforces destination IPs for all browser targets, including
    workers and redirects. This gate ensures an ignored page_setup never navigates.
    """
    def __init__(self):
        self.failed = False
        self.installed = False
        self.context = None
        self.page = None

    def _stop(self):
        self.failed = True
        if self.context is not None:
            try:
                self.context.close()
            except Exception:
                pass
        raise BrowserGuardError() from None

    def _callback(self, fn):
        def call(value):
            try:
                return fn(value)
            except Exception as error:
                # Cancellation is normal: a page may remove a frame/request while
                # it is paused. Do not turn that race into a context-wide failure.
                message = str(error)
                if any(reason in message for reason in (
                        "Target page, context or browser has been closed",
                        "Invalid InterceptionId", "Invalid interceptionId",
                        "Request is already handled", "Request was cancelled")):
                    return
                self._stop()
        return call

    def install(self, context):
        self.context = context
        try:
            context.route("**/*", self._callback(self._route))
            context.route_web_socket("**/*", self._callback(lambda route: route.close()))
        except Exception:
            self._stop()

    def setup(self, page):
        if self.context is None or page.context != self.context or self.failed:
            self._stop()
        self.page = page
        self.installed = True

    def _route(self, route):
        try:
            owned = self.installed and route.request.frame.page == self.page
        except Exception:
            # Workers have no frame; their connections are guarded by the proxy.
            owned = self.installed
        if owned:
            route.continue_()
        else:
            route.abort()


def _browser_options(timeout_ms, proxy, profile, guard, solve_cloudflare=False):
    options = dict(solve_cloudflare=solve_cloudflare, retries=1, retry_delay=0,
                   timeout=timeout_ms, google_search=False, block_webrtc=True,
                   page_setup=guard.setup,
                   additional_args={"service_workers": "block", "ignore_https_errors": False})
    configured = browser_proxy(proxy)
    configured["bypass"] = "<-loopback>"
    options["proxy"] = configured
    # Scrapling's ProxyDict normalizer drops Playwright's bypass field. Pass the
    # complete context option through the documented additional_args override.
    options["additional_args"]["proxy"] = configured
    return options


def verify_context_policy(session, proxy):
    """Fail closed if a runtime drops or changes the required context policy.

    Scrapling's normal ProxyDict currently drops bypass. The documented context
    override preserves it, but an incompatible runtime must not silently remove
    loopback protection. Check before any extraction navigation is possible.
    """
    expected = browser_proxy(proxy)
    expected["bypass"] = "<-loopback>"
    options = getattr(session, "_context_options", None)
    if (not isinstance(options, dict) or options.get("proxy") != expected
            or options.get("service_workers") != "block"
            or options.get("ignore_https_errors") is not False):
        raise BrowserGuardError()


def fetch_browser(url, timeout_ms=60000, proxy=None, profile=None, cdp_url=None, solve_cloudflare=False):
    """Attach only to Go's private, owned Chromium; never launch a fallback."""
    parsed = urlsplit(url)
    if parsed.scheme not in ("http", "https") or not parsed.hostname:
        raise BrowserFetchError()
    guard = BrowserGuard()
    if not cdp_url or not proxy:
        raise BrowserGuardError()
    options = _browser_options(timeout_ms, proxy, profile, guard, solve_cloudflare)
    try:
        from scrapling.fetchers import RemoteSession
    except Exception:
        raise BrowserUnavailable() from None
    try:
        with RemoteSession(cdp_url=cdp_url, new_context=True, **options) as session:
            # Install before calling upstream fetch, not inside its callback.
            verify_context_policy(session, proxy)
            guard.install(session.context)
            page = session.fetch(url)
            if guard.failed or not guard.installed:
                raise BrowserGuardError()
            return page
    except Exception:
        raise BrowserFetchError() from None


def check_browser(profile, proxy=None, cdp_url=None):
    guard = BrowserGuard()
    if not cdp_url or not proxy:
        raise BrowserGuardError()
    options = _browser_options(5000, proxy, profile, guard)
    try:
        from scrapling.fetchers import RemoteSession
        with RemoteSession(cdp_url=cdp_url, new_context=True, **options) as session:
            verify_context_policy(session, proxy)
            guard.install(session.context)
            page = session.fetch("http://nole-readiness.invalid/")
            if (guard.failed or not guard.installed or getattr(page, "status", None) != 200
                    or "nole browser proxy ready" not in getattr(page, "text", "")):
                raise BrowserGuardError()
    except Exception:
        raise BrowserUnavailable() from None
