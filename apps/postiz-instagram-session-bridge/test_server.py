import http.client
import json
import threading
import unittest

from server import BridgeHTTPServer, RequestHandler


class FakeBridge:
    session_id = "configured"

    def __init__(self):
        self.calls = []

    def post(self, posts, dry_run=False):
        self.calls.append((posts, dry_run))
        return [
            {
                "id": posts[0]["id"],
                "postId": "result-id",
                "releaseURL": "https://www.instagram.com/p/RESULT/",
                "status": "posted",
            }
        ]


class ServerTest(unittest.TestCase):
    def setUp(self):
        self.bridge = FakeBridge()
        self.server = BridgeHTTPServer(
            ("127.0.0.1", 0),
            RequestHandler,
            bridge=self.bridge,
            bearer_token="bridge-token",
        )
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        self.port = self.server.server_address[1]

    def request(self, method, path, body=None, headers=None):
        connection = http.client.HTTPConnection("127.0.0.1", self.port, timeout=2)
        connection.request(method, path, body=body, headers=headers or {})
        response = connection.getresponse()
        payload = json.loads(response.read())
        connection.close()
        return response.status, payload

    def test_health_reports_session_configuration(self):
        status, payload = self.request("GET", "/healthz")

        self.assertEqual(status, 200)
        self.assertEqual(payload, {"status": "ok", "configured": True})

    def test_ready_rejects_an_unconfigured_session(self):
        self.bridge.session_id = ""

        status, payload = self.request("GET", "/readyz")

        self.assertEqual(status, 503)
        self.assertEqual(payload, {"status": "unavailable", "configured": False})

    def test_post_requires_bridge_token(self):
        status, payload = self.request(
            "POST",
            "/post",
            body=json.dumps({"posts": []}),
            headers={"Content-Type": "application/json"},
        )

        self.assertEqual(status, 401)
        self.assertEqual(payload, {"error": "unauthorized"})

    def test_post_forwards_posts_and_dry_run(self):
        posts = [{"id": "post-1", "message": "Caption", "media": [{"path": "x"}]}]
        status, payload = self.request(
            "POST",
            "/post",
            body=json.dumps({"posts": posts, "dryRun": True}),
            headers={
                "Authorization": "Bearer bridge-token",
                "Content-Type": "application/json",
            },
        )

        self.assertEqual(status, 200)
        self.assertEqual(payload["results"][0]["postId"], "result-id")
        self.assertEqual(self.bridge.calls, [(posts, True)])

    def test_post_rejects_invalid_json(self):
        status, payload = self.request(
            "POST",
            "/post",
            body="not-json",
            headers={
                "Authorization": "Bearer bridge-token",
                "Content-Type": "application/json",
            },
        )

        self.assertEqual(status, 400)
        self.assertEqual(payload, {"error": "invalid json"})


if __name__ == "__main__":
    unittest.main()
