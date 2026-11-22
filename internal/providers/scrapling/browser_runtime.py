"""Optional Scrapling browser support, embedded into the extraction subprocess.

The Go caller owns stage/total deadlines and process cleanup. DNS checks here
are preflight checks, not socket pinning: Chromium or a proxy can resolve later.
"""
import ipaddress
import json
import re
import socket
import sys
import tempfile
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


# Match safenet's private/reserved ranges explicitly. Python's is_private also
# includes unrelated ranges and has changed across Python releases.
_BROWSER_BLOCKED = tuple(ipaddress.ip_network(cidr) for cidr in (
    "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7",
    "100.64.0.0/10", "0.0.0.0/8", "192.0.0.0/24", "198.18.0.0/15",
    "64:ff9b::/96", "2001:db8::/32",
))


def _browser_public_ip(raw):
    ip = ipaddress.ip_address(raw)
    if isinstance(ip, ipaddress.IPv6Address) and ip.ipv4_mapped:
        ip = ip.ipv4_mapped
    if ip.is_loopback or ip.is_link_local or ip.is_multicast or ip.is_unspecified:
        return False
    if any(ip in network for network in _BROWSER_BLOCKED):
        return False
    if isinstance(ip, ipaddress.IPv6Address):
        if int(ip) >> 32 == 0:
            return _browser_public_ip(str(ipaddress.IPv4Address(int(ip))))
        if ip.sixtofour:
            return _browser_public_ip(str(ip.sixtofour))
    return True


def browser_public_url(url):
    """Payload-free equivalent of the existing best-effort public URL check."""
    try:
        parsed = urlsplit(url)
        host = parsed.hostname
        if parsed.scheme not in ("http", "https") or not host or "%" in host:
            return False
        _ = parsed.port  # Reject malformed ports without exposing the URL.
        if host.lower().rstrip(".") in ("localhost", "localhost.localdomain"):
            return False
        try:
            ipaddress.ip_address(host)
        except ValueError:
            if all(re.fullmatch(r"[0-9]+|0[xX][0-9a-fA-F]+", part)
                   for part in host.rstrip(".").split(".")):
                return False
            answers = socket.getaddrinfo(host, None, type=socket.SOCK_STREAM)
            return bool(answers) and all(_browser_public_ip(answer[4][0]) for answer in answers)
        return _browser_public_ip(host)
    except Exception:
        return False


def browser_proxy(proxy):
    """Translate the predecessor's validated private proxy value for Chromium.

    Ambient proxy filtering belongs to the Go subprocess launcher. Do not change
    schemes, discard credentials, or retry directly when this mode is unsupported.
    """
    if not proxy:
        return None
    try:
        parsed = urlsplit(proxy)
        if parsed.scheme not in ("http", "https", "socks4", "socks5") or not parsed.hostname:
            raise ValueError()
        if parsed.scheme.startswith("socks") and parsed.username is not None:
            raise ValueError()
        host = parsed.hostname
        if ":" in host:
            host = "[" + host + "]"
        server = parsed.scheme + "://" + host
        if parsed.port is not None:
            server += ":" + str(parsed.port)
        result = {"server": server}
        if parsed.username is not None:
            result.update(username=unquote(parsed.username), password=unquote(parsed.password or ""))
        return result
    except Exception:
        raise BrowserProxyError() from None


class BrowserGuard:
    """Guard this extraction page and its iframe descendants before requests."""
    _fetch = {"patterns": [{"urlPattern": "*", "requestStage": "Request"}]}
    _attach = {"autoAttach": True, "waitForDebuggerOnStart": True, "flatten": False,
               "filter": [{"type": "iframe"}, {"exclude": True}]}

    def __init__(self, validate=None):
        self.validate = validate or browser_public_url
        self.failed = False
        self.page = None
        self.session = None
        self.serial = 0
        self.pending = {}
        self.frames = set()

    def _stop(self):
        if not self.failed:
            self.failed = True
            if self.page is not None:
                try:
                    self.page.context.close()
                except Exception:
                    pass
        raise BrowserGuardError() from None

    def _callback(self, fn):
        # Playwright inspects callback arity before dispatching route/request.
        # Keep the one-argument contract; *args would forward both to _route.
        def call(value):
            try:
                return fn(value)
            except Exception:
                self._stop()
        return call

    def setup(self, page):
        self.page = page
        try:
            page.context.route("**/*", self._callback(self._route))
            # Fetch interception does not cover WebSocket redirects. Extraction
            # does not open this unguarded channel; no connection is established.
            page.context.route_web_socket("**/*", self._callback(lambda route: route.close()))
            self.session = page.context.new_cdp_session(page)
            self.session.on("Fetch.requestPaused", self._callback(lambda event: self._paused((), event)))
            self.session.on("Target.attachedToTarget", self._callback(lambda event: self._attached((), event)))
            self.session.on("Target.detachedFromTarget", self._callback(lambda event: self._detached((), event)))
            self.session.on("Target.receivedMessageFromTarget", self._callback(lambda event: self._received((), event)))
            self.session.send("Fetch.enable", self._fetch)
            self.session.send("Target.setAutoAttach", self._attach)
        except Exception:
            self._stop()

    def _route(self, route):
        try:
            owned = route.request.frame.page == self.page
        except Exception:
            owned = False
        if owned:
            route.continue_()
        else:
            route.abort()

    def _id(self):
        self.serial += 1
        return self.serial

    def _send(self, path, method, params, phase=None):
        if not path:
            self.session.send(method, params)
            return
        ident = self._id()
        if phase:
            self.pending[(path, ident)] = phase
        message = {"id": ident, "method": method, "params": params}
        # Python's public CDPSession cannot address flattened child sessions.
        # Keep this non-flat transport local to iframe guard setup and requests.
        for sid in reversed(path):
            message = {"id": self._id(), "method": "Target.sendMessageToTarget", "params": {
                "sessionId": sid, "message": json.dumps(message)}}
        self.session.send(message["method"], message["params"])

    def _paused(self, path, event):
        if self.validate(event["request"]["url"]):
            self._send(path, "Fetch.continueRequest", {"requestId": event["requestId"]})
        else:
            self._send(path, "Fetch.failRequest", {"requestId": event["requestId"], "errorReason": "BlockedByClient"})

    def _attached(self, parent, event):
        if event["targetInfo"]["type"] != "iframe" or not event.get("waitingForDebugger"):
            self._stop()
        path = parent + (event["sessionId"],)
        self.frames.add(path)
        self._send(path, "Fetch.enable", self._fetch, "fetch")

    def _detached(self, parent, event):
        path = parent + (event["sessionId"],)
        self.frames = {p for p in self.frames if p[:len(path)] != path}
        self.pending = {key: phase for key, phase in self.pending.items() if key[0][:len(path)] != path}

    def _received(self, parent, event):
        path = parent + (event["sessionId"],)
        if path not in self.frames:
            return  # Late acknowledgements from a detached target cannot resume it.
        message = json.loads(event["message"])
        if "error" in message:
            self._stop()
        phase = self.pending.pop((path, message.get("id")), None)
        if phase == "fetch":
            self._send(path, "Target.setAutoAttach", self._attach, "autoattach")
        elif phase == "autoattach":
            self._send(path, "Runtime.runIfWaitingForDebugger", {})
        method, params = message.get("method"), message.get("params", {})
        if method == "Target.attachedToTarget":
            self._attached(path, params)
        elif method == "Target.detachedFromTarget":
            self._detached(path, params)
        elif method == "Target.receivedMessageFromTarget":
            self._received(path, params)
        elif method == "Fetch.requestPaused":
            self._paused(path, params)


def _browser_options(timeout_ms, proxy, profile, guard):
    options = dict(headless=True, solve_cloudflare=True, retries=1, retry_delay=0,
                   timeout=timeout_ms, google_search=False, block_webrtc=True,
                   page_setup=guard.setup, user_data_dir=profile,
                   additional_args={"service_workers": "block", "ignore_https_errors": False})
    configured = browser_proxy(proxy)
    if configured is not None:
        options["proxy"] = configured
    return options


def fetch_browser(url, timeout_ms=60000, proxy=None, profile=None):
    """Return a page to the existing classifier/extractor, never page.meta.

    The caller enforces the deadline even when Scrapling raises its solver timeout
    to 60s. It also owns the profile directory for cleanup after a killed child.
    """
    guard = BrowserGuard()
    options = _browser_options(timeout_ms, proxy, profile, guard)
    if not browser_public_url(url):
        raise BrowserFetchError()
    try:
        from scrapling.fetchers import StealthyFetcher
    except Exception:
        raise BrowserUnavailable() from None
    try:
        if profile is None:
            with tempfile.TemporaryDirectory(prefix="nole-browser-") as owned:
                options["user_data_dir"] = owned
                page = StealthyFetcher.fetch(url, **options)
        else:
            page = StealthyFetcher.fetch(url, **options)
        if guard.failed:
            raise BrowserGuardError()
        return page
    except Exception:
        raise BrowserFetchError() from None


def check_browser(profile, proxy=None):
    """Launch, install guards, and close a fresh browser without navigation."""
    guard = BrowserGuard()
    options = _browser_options(5000, proxy, profile, guard)
    try:
        from scrapling.fetchers import StealthySession
        with StealthySession(**options) as session:
            guard.setup(session.context.new_page())
    except Exception:
        raise BrowserUnavailable() from None
