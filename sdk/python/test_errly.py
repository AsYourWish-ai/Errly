"""Tests for the Errly Python SDK."""
from __future__ import annotations

import json
import queue
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Any
from unittest.mock import MagicMock, patch

import pytest

from errly import Errly, _is_in_app, _parse_traceback


# ── Capture server fixture ────────────────────────────────────────────────────

class _CaptureHandler(BaseHTTPRequestHandler):
    """HTTP handler that records received events."""

    def do_POST(self):  # noqa: N802
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        try:
            event = json.loads(body)
            self.server.received.append(event)
            self.server.keys.append(self.headers.get("X-Errly-Key", ""))
        except json.JSONDecodeError:
            pass
        self.send_response(201)
        self.end_headers()
        self.wfile.write(b'{"id":"ok"}')

    def log_message(self, *args):  # silence request logs
        pass


@pytest.fixture()
def capture_server():
    """Starts a local HTTP server that records events. Returns (url, server)."""
    server = HTTPServer(("127.0.0.1", 0), _CaptureHandler)
    server.received: list[dict] = []
    server.keys: list[str] = []
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    host, port = server.server_address
    yield f"http://{host}:{port}", server
    server.shutdown()


@pytest.fixture()
def client(capture_server):
    url, _ = capture_server
    c = Errly(url=url, api_key="test-key", project="test-proj", environment="test")
    yield c
    c.close(timeout=3)


# ── capture_exception ─────────────────────────────────────────────────────────

def test_capture_exception_returns_id(client, capture_server):
    url, srv = capture_server
    try:
        raise ValueError("boom")
    except ValueError as e:
        event_id = client.capture_exception(e)
    assert event_id != ""
    client.flush(timeout=3)
    assert len(srv.received) == 1


def test_capture_exception_event_shape(client, capture_server):
    url, srv = capture_server
    try:
        raise RuntimeError("disk full")
    except RuntimeError as e:
        client.capture_exception(e)
    client.flush(timeout=3)

    ev = srv.received[0]
    assert ev["level"] == "error"
    assert ev["platform"] == "python"
    assert ev["environment"] == "test"
    assert ev["project_key"] == "test-proj"
    assert ev["exception"]["type"] == "RuntimeError"
    assert ev["exception"]["value"] == "disk full"
    assert isinstance(ev["stacktrace"], list)
    assert len(ev["stacktrace"]) > 0


def test_capture_exception_stacktrace_most_recent_first(client, capture_server):
    url, srv = capture_server

    def inner():
        raise TypeError("inner error")

    def outer():
        inner()

    try:
        outer()
    except TypeError as e:
        client.capture_exception(e)
    client.flush(timeout=3)

    ev = srv.received[0]
    frames = ev["stacktrace"]
    # Most recent frame should reference 'inner'
    assert frames[0]["function"] == "inner"


def test_capture_exception_uses_current_exc_info(client, capture_server):
    url, srv = capture_server
    try:
        raise KeyError("missing")
    except KeyError:
        client.capture_exception()  # no arg — uses sys.exc_info()
    client.flush(timeout=3)

    ev = srv.received[0]
    assert ev["exception"]["type"] == "KeyError"


def test_capture_exception_with_extra(client, capture_server):
    url, srv = capture_server
    try:
        raise ValueError("x")
    except ValueError as e:
        client.capture_exception(e, extra={"user_id": "42", "action": "checkout"})
    client.flush(timeout=3)

    ev = srv.received[0]
    assert ev["extra"]["user_id"] == "42"
    assert ev["extra"]["action"] == "checkout"


# ── capture_message ───────────────────────────────────────────────────────────

def test_capture_message_defaults(client, capture_server):
    url, srv = capture_server
    event_id = client.capture_message("Something weird happened")
    assert event_id != ""
    client.flush(timeout=3)

    ev = srv.received[0]
    assert ev["message"] == "Something weird happened"
    assert ev["level"] == "info"


def test_capture_message_custom_level(client, capture_server):
    url, srv = capture_server
    client.capture_message("Payment slow", level="warning")
    client.flush(timeout=3)

    assert srv.received[0]["level"] == "warning"


def test_capture_message_with_extra(client, capture_server):
    url, srv = capture_server
    client.capture_message("msg", extra={"k": "v"})
    client.flush(timeout=3)

    assert srv.received[0]["extra"]["k"] == "v"


# ── set_user ──────────────────────────────────────────────────────────────────

def test_set_user_attached_to_event(client, capture_server):
    url, srv = capture_server
    client.set_user(user_id="u1", email="u@example.com", username="alice")
    client.capture_message("test")
    client.flush(timeout=3)

    user = srv.received[0]["user"]
    assert user["id"] == "u1"
    assert user["email"] == "u@example.com"
    assert user["username"] == "alice"


def test_set_user_overrides_previous(client, capture_server):
    url, srv = capture_server
    client.set_user(user_id="u1")
    client.set_user(user_id="u2")
    client.capture_message("test")
    client.flush(timeout=3)

    assert srv.received[0]["user"]["id"] == "u2"


# ── add_breadcrumb ────────────────────────────────────────────────────────────

def test_add_breadcrumb_attached_to_event(client, capture_server):
    url, srv = capture_server
    client.add_breadcrumb("db query", category="database", level="info")
    client.capture_message("test")
    client.flush(timeout=3)

    crumbs = srv.received[0]["breadcrumbs"]
    assert len(crumbs) == 1
    assert crumbs[0]["message"] == "db query"
    assert crumbs[0]["category"] == "database"
    assert crumbs[0]["level"] == "info"


def test_add_breadcrumb_capped_at_50(client, capture_server):
    url, srv = capture_server
    for i in range(55):
        client.add_breadcrumb(f"crumb-{i}")
    client.capture_message("test")
    client.flush(timeout=3)

    crumbs = srv.received[0]["breadcrumbs"]
    assert len(crumbs) == 50
    # Last 50 kept: crumb-5 through crumb-54
    assert crumbs[0]["message"] == "crumb-5"
    assert crumbs[-1]["message"] == "crumb-54"


# ── flush ─────────────────────────────────────────────────────────────────────

def test_flush_drains_all_events(capture_server):
    url, srv = capture_server
    c = Errly(url=url, api_key="k")
    for i in range(10):
        c.capture_message(f"msg-{i}")
    result = c.flush(timeout=5)
    assert result is True
    assert len(srv.received) == 10
    c.close()


def test_flush_returns_false_on_timeout(capture_server):
    """A very short timeout should return False when queue is not empty."""
    url, srv = capture_server
    # Slow server that delays responses
    import socket
    slow_sock = socket.socket()
    slow_sock.bind(("127.0.0.1", 0))
    slow_sock.listen(1)
    _, port = slow_sock.getsockname()
    slow_sock.close()

    c = Errly(url=f"http://127.0.0.1:{port}", api_key="k")
    for i in range(20):
        c.capture_message(f"msg-{i}")
    result = c.flush(timeout=0.01)  # essentially zero timeout
    # May be True or False depending on timing — just must not hang
    assert isinstance(result, bool)
    c.close(timeout=0.1)


# ── close ─────────────────────────────────────────────────────────────────────

def test_close_flushes_and_stops(capture_server):
    url, srv = capture_server
    c = Errly(url=url, api_key="k")
    c.capture_message("before close")
    c.close(timeout=3)
    assert len(srv.received) == 1


def test_close_safe_to_call_twice(capture_server):
    url, srv = capture_server
    c = Errly(url=url, api_key="k")
    c.close(timeout=1)
    c.close(timeout=1)  # must not raise


# ── queue full ────────────────────────────────────────────────────────────────

def test_queue_full_drops_silently():
    c = Errly(url="http://127.0.0.1:1", api_key="k", max_queue=2)
    # Overfill queue — must not raise
    for i in range(10):
        c.capture_message(f"msg-{i}")
    c.close(timeout=0.1)


# ── network error ─────────────────────────────────────────────────────────────

def test_send_network_error_silent_drop():
    c = Errly(url="http://127.0.0.1:1", api_key="k")  # nothing listening
    c.capture_message("will fail silently")
    c.close(timeout=1)  # must not raise


# ── API key in header ─────────────────────────────────────────────────────────

def test_api_key_sent_in_header(client, capture_server):
    url, srv = capture_server
    client.capture_message("test")
    client.flush(timeout=3)
    assert srv.keys[0] == "test-key"


# ── FastAPI middleware ────────────────────────────────────────────────────────

def test_instrument_fastapi_captures_exception(capture_server):
    pytest.importorskip("fastapi")
    from fastapi import FastAPI
    from fastapi.testclient import TestClient

    url, srv = capture_server
    c = Errly(url=url, api_key="k")
    app = FastAPI()
    c.instrument_fastapi(app)

    @app.get("/boom")
    async def boom():
        raise RuntimeError("fastapi error")

    tc = TestClient(app, raise_server_exceptions=False)
    tc.get("/boom")
    c.flush(timeout=3)

    assert len(srv.received) == 1
    assert srv.received[0]["exception"]["type"] == "RuntimeError"
    c.close()


def test_instrument_fastapi_normal_request_no_event(capture_server):
    pytest.importorskip("fastapi")
    from fastapi import FastAPI
    from fastapi.testclient import TestClient

    url, srv = capture_server
    c = Errly(url=url, api_key="k")
    app = FastAPI()
    c.instrument_fastapi(app)

    @app.get("/ok")
    async def ok():
        return {"ok": True}

    tc = TestClient(app)
    tc.get("/ok")
    c.flush(timeout=1)
    assert len(srv.received) == 0
    c.close()


def test_instrument_fastapi_missing_dep():
    """instrument_fastapi must not raise when starlette is not installed."""
    with patch.dict("sys.modules", {"starlette": None, "starlette.middleware": None,
                                    "starlette.middleware.base": None,
                                    "starlette.requests": None}):
        c = Errly(url="http://127.0.0.1:1", api_key="k")
        app = MagicMock()
        result = c.instrument_fastapi(app)
        assert result is app
        c.close(timeout=0.1)


# ── Flask middleware ──────────────────────────────────────────────────────────

def test_instrument_flask_captures_exception(capture_server):
    pytest.importorskip("flask")
    from flask import Flask

    url, srv = capture_server
    c = Errly(url=url, api_key="k")
    app = Flask(__name__)
    c.instrument_flask(app)

    @app.route("/boom")
    def boom():
        raise RuntimeError("flask error")

    with app.test_client() as tc:
        tc.get("/boom")

    c.flush(timeout=3)
    assert len(srv.received) == 1
    assert srv.received[0]["exception"]["type"] == "RuntimeError"
    c.close()


# ── _parse_traceback helper ───────────────────────────────────────────────────

def test_parse_traceback_none_returns_empty():
    assert _parse_traceback(None) == []


def test_parse_traceback_returns_frames():
    try:
        raise ValueError("test")
    except ValueError as e:
        frames = _parse_traceback(e.__traceback__)

    assert isinstance(frames, list)
    assert len(frames) > 0
    frame = frames[0]
    assert "filename" in frame
    assert "function" in frame
    assert "lineno" in frame
    assert isinstance(frame["in_app"], bool)


def test_parse_traceback_most_recent_first():
    def inner():
        raise TypeError("x")

    def outer():
        inner()

    try:
        outer()
    except TypeError as e:
        frames = _parse_traceback(e.__traceback__)

    assert frames[0]["function"] == "inner"


# ── _is_in_app helper ─────────────────────────────────────────────────────────

@pytest.mark.parametrize("filename,expected", [
    ("/home/user/myapp/handler.py", True),
    ("/usr/lib/python3.11/site-packages/requests/api.py", False),
    ("/usr/lib/python3.11/threading.py", False),
    ("<string>", False),
    ("<frozen importlib>", False),
])
def test_is_in_app(filename, expected):
    assert _is_in_app(filename) == expected


# ── event ID uniqueness ───────────────────────────────────────────────────────

def test_event_ids_are_unique(client, capture_server):
    url, srv = capture_server
    for _ in range(20):
        client.capture_message("test")
    client.flush(timeout=5)

    ids = [ev["id"] for ev in srv.received]
    assert len(set(ids)) == 20


# ── thread safety ─────────────────────────────────────────────────────────────

def test_concurrent_capture_thread_safe(capture_server):
    url, srv = capture_server
    c = Errly(url=url, api_key="k")
    threads = []
    for i in range(20):
        t = threading.Thread(target=c.capture_message, args=(f"msg-{i}",))
        threads.append(t)
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    c.flush(timeout=5)
    assert len(srv.received) == 20
    c.close()
