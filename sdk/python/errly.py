"""
Errly Python SDK — lightweight error monitoring

Usage:
    from errly import Errly

    errly = Errly(
        url="http://localhost:5080",
        api_key="your-api-key",
        project="my-service",
        environment="production",
        release="v1.2.3",
    )

    # Capture an exception
    try:
        risky_operation()
    except Exception as e:
        errly.capture_exception(e)

    # Capture a message
    errly.capture_message("Something weird happened", level="warning")

    # FastAPI / Flask middleware
    app = errly.instrument_fastapi(app)
"""

from __future__ import annotations

import json
import queue
import sys
import threading
import time
import traceback
import uuid
from datetime import datetime, timezone
from typing import Any, Optional
from urllib import request as urllib_request
from urllib.error import URLError


class Errly:
    def __init__(
        self,
        url: str,
        api_key: str = "",
        project: str = "",
        environment: str = "production",
        release: str = "",
        max_queue: int = 512,
    ):
        self.url = url.rstrip("/")
        self.api_key = api_key
        self.project = project
        self.environment = environment
        self.release = release

        self._queue: queue.Queue = queue.Queue(maxsize=max_queue)
        self._user: Optional[dict] = None
        self._breadcrumbs: list[dict] = []
        self._lock = threading.RLock()
        self._stop = threading.Event()

        # Background worker thread — daemon so it never blocks interpreter exit,
        # but flush() can be called explicitly to drain before shutdown.
        self._worker = threading.Thread(target=self._run, daemon=True)
        self._worker.start()

    # ── Public API ────────────────────────────────────────────────────────────

    def capture_exception(
        self,
        exc: Optional[BaseException] = None,
        extra: Optional[dict] = None,
    ) -> str:
        """Capture an exception with its stack trace."""
        if exc is None:
            exc_type, exc, tb = sys.exc_info()
        else:
            exc_type = type(exc)
            tb = exc.__traceback__

        event = self._new_event("error")
        event["exception"] = {
            "type": exc_type.__name__ if exc_type else "Exception",
            "value": str(exc),
            "module": getattr(exc_type, "__module__", ""),
        }
        event["stacktrace"] = _parse_traceback(tb)
        if extra:
            event["extra"] = extra

        self._enqueue(event)
        return event["id"]

    def capture_message(
        self,
        message: str,
        level: str = "info",
        extra: Optional[dict] = None,
    ) -> str:
        """Capture a plain message."""
        event = self._new_event(level)
        event["message"] = message
        if extra:
            event["extra"] = extra
        self._enqueue(event)
        return event["id"]

    def set_user(self, user_id: str = "", email: str = "", username: str = "") -> None:
        """Set the current user for all subsequent events."""
        with self._lock:
            self._user = {"id": user_id, "email": email, "username": username}

    def add_breadcrumb(
        self,
        message: str,
        category: str = "default",
        level: str = "info",
        crumb_type: str = "default",
    ) -> None:
        """Add a breadcrumb to the trail (max 50 kept)."""
        with self._lock:
            self._breadcrumbs.append({
                "timestamp": datetime.now(timezone.utc).isoformat(),
                "type": crumb_type,
                "category": category,
                "message": message,
                "level": level,
            })
            if len(self._breadcrumbs) > 50:
                self._breadcrumbs = self._breadcrumbs[-50:]

    def flush(self, timeout: float = 5.0) -> bool:
        """Wait for all queued events to be sent within *timeout* seconds.

        Returns True if the queue was fully drained, False if the timeout elapsed.
        """
        # queue.join() has no timeout — use a polling approach that respects timeout.
        deadline = time.monotonic() + timeout
        while not self._queue.empty():
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                return False
            time.sleep(min(0.05, remaining))
        # One final join attempt on an effectively-empty queue to process
        # items that were in-flight when empty() was last checked.
        self._queue.join()
        return True

    def close(self, timeout: float = 5.0) -> bool:
        """Flush pending events and stop the background worker."""
        self._stop.set()
        return self.flush(timeout)

    # ── Framework integrations ────────────────────────────────────────────────

    def instrument_fastapi(self, app: Any) -> Any:
        """Add Errly error capture to a FastAPI app."""
        try:
            from starlette.middleware.base import BaseHTTPMiddleware
            from starlette.requests import Request

            errly_client = self

            class ErrlyMiddleware(BaseHTTPMiddleware):
                async def dispatch(self, request: Request, call_next):
                    try:
                        return await call_next(request)
                    except Exception as exc:
                        event = errly_client._new_event("error")
                        event["exception"] = {
                            "type": type(exc).__name__,
                            "value": str(exc),
                        }
                        event["stacktrace"] = _parse_traceback(exc.__traceback__)
                        event["request"] = {
                            "url": str(request.url),
                            "method": request.method,
                            "headers": {"user-agent": request.headers.get("user-agent", "")},
                        }
                        errly_client._enqueue(event)
                        raise

            app.add_middleware(ErrlyMiddleware)
        except ImportError:
            pass
        return app

    def instrument_flask(self, app: Any) -> Any:
        """Add Errly error capture to a Flask app."""
        errly_client = self

        @app.errorhandler(Exception)
        def handle_exception(exc: Exception):
            event = errly_client._new_event("error")
            event["exception"] = {"type": type(exc).__name__, "value": str(exc)}
            event["stacktrace"] = _parse_traceback(exc.__traceback__)
            errly_client._enqueue(event)
            raise exc

        return app

    # ── Internal ──────────────────────────────────────────────────────────────

    def _new_event(self, level: str) -> dict:
        with self._lock:
            crumbs = list(self._breadcrumbs)
            user = dict(self._user) if self._user else None

        return {
            "id": str(uuid.uuid4()).replace("-", ""),
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "level": level,
            "message": "",
            "platform": "python",
            "environment": self.environment,
            "release": self.release,
            "project_key": self.project,
            "breadcrumbs": crumbs,
            "user": user,
        }

    def _enqueue(self, event: dict) -> None:
        try:
            self._queue.put_nowait(event)
        except queue.Full:
            pass  # Drop if queue is full

    def _run(self) -> None:
        while not self._stop.is_set():
            try:
                event = self._queue.get(timeout=0.5)
                self._send(event)
                self._queue.task_done()
            except queue.Empty:
                continue
        # Drain remaining items after stop signal
        while True:
            try:
                event = self._queue.get_nowait()
                self._send(event)
                self._queue.task_done()
            except queue.Empty:
                break

    def _send(self, event: dict) -> None:
        try:
            data = json.dumps(event).encode("utf-8")
            req = urllib_request.Request(
                f"{self.url}/api/v1/events",
                data=data,
                headers={
                    "Content-Type": "application/json",
                    "X-Errly-Key": self.api_key,
                },
                method="POST",
            )
            with urllib_request.urlopen(req, timeout=5) as resp:
                resp.read()
        except (URLError, OSError):
            pass  # Network errors are silently dropped


# ── Helpers ───────────────────────────────────────────────────────────────────

def _parse_traceback(tb) -> list[dict]:
    """Parse a traceback into Errly stack frames."""
    if tb is None:
        return []

    frames = []
    for frame_info in traceback.extract_tb(tb):
        frames.append({
            "filename": frame_info.filename,
            "function": frame_info.name,
            "lineno": frame_info.lineno,
            "colno": 0,
            "in_app": _is_in_app(frame_info.filename),
        })
    return list(reversed(frames))  # Most recent first


def _is_in_app(filename: str) -> bool:
    return (
        "site-packages" not in filename
        and "lib/python" not in filename
        and "<" not in filename
    )
