import hmac
import json
import logging
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from bridge import BridgeError, bridge_from_environment


MAX_REQUEST_BYTES = 2 << 20


class BridgeHTTPServer(ThreadingHTTPServer):
    def __init__(self, server_address, handler_class, bridge, bearer_token=""):
        super().__init__(server_address, handler_class)
        self.bridge = bridge
        self.bearer_token = bearer_token.strip()


class RequestHandler(BaseHTTPRequestHandler):
    server: BridgeHTTPServer

    def do_GET(self):
        if self.path == "/healthz":
            self.write_json(
                200,
                {
                    "status": "ok",
                    "configured": bool(self.server.bridge.session_id),
                },
            )
            return
        if self.path == "/readyz":
            configured = bool(self.server.bridge.session_id)
            self.write_json(
                200 if configured else 503,
                {
                    "status": "ok" if configured else "unavailable",
                    "configured": configured,
                },
            )
            return
        self.write_json(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/post":
            self.write_json(404, {"error": "not found"})
            return
        if not self.authorized():
            self.write_json(401, {"error": "unauthorized"})
            return

        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self.write_json(400, {"error": "invalid content length"})
            return
        if length <= 0 or length > MAX_REQUEST_BYTES:
            self.write_json(413, {"error": "request body is too large or empty"})
            return

        try:
            request = json.loads(self.rfile.read(length))
        except (json.JSONDecodeError, UnicodeDecodeError):
            self.write_json(400, {"error": "invalid json"})
            return

        posts = request.get("posts") or self.normalized_single_post(request)
        try:
            results = self.server.bridge.post(posts, dry_run=bool(request.get("dryRun")))
        except BridgeError as error:
            self.write_json(400, {"error": str(error)})
            return
        except Exception:
            logging.exception("Instagram bridge post failed")
            self.write_json(502, {"error": "Instagram post failed"})
            return
        self.write_json(200, {"results": results})

    def normalized_single_post(self, request):
        if not request.get("message") and not request.get("media"):
            return []
        return [
            {
                "id": "single",
                "message": request.get("message", ""),
                "media": request.get("media", []),
                "settings": request.get("settings", {}),
            }
        ]

    def authorized(self):
        if not self.server.bearer_token:
            return True
        expected = f"Bearer {self.server.bearer_token}"
        return hmac.compare_digest(self.headers.get("Authorization", ""), expected)

    def write_json(self, status, value):
        payload = json.dumps(value, separators=(",", ":")).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, message, *args):
        logging.info("%s - %s", self.address_string(), message % args)


def main():
    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO"),
        format="%(asctime)s %(levelname)s %(message)s",
    )
    bridge = bridge_from_environment()
    server = BridgeHTTPServer(
        ("0.0.0.0", int(os.environ.get("PORT", "8080"))),
        RequestHandler,
        bridge=bridge,
        bearer_token=os.environ.get("POSTIZ_INSTAGRAM_BRIDGE_TOKEN", ""),
    )
    logging.info(
        "starting Postiz Instagram session bridge on port %s (configured=%s)",
        server.server_address[1],
        bool(bridge.session_id),
    )
    server.serve_forever()


if __name__ == "__main__":
    main()
