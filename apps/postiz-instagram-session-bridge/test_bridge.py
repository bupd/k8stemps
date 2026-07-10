import base64
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace

from bridge import BridgeError, InstagramBridge, MediaResolver


class FakeInstagramClient:
    def __init__(self):
        self.calls = []

    def login_by_sessionid(self, session_id):
        self.calls.append(("login", session_id))
        return True

    def photo_upload(self, path, caption):
        self.calls.append(("photo_upload", Path(path), caption))
        return SimpleNamespace(pk="photo-pk", code="PHOTO123")

    def album_upload(self, paths, caption):
        self.calls.append(("album_upload", [Path(path) for path in paths], caption))
        return SimpleNamespace(pk="album-pk", code="ALBUM123")

    def media_comment(self, media_pk, message):
        self.calls.append(("media_comment", media_pk, message))
        return SimpleNamespace(pk=f"comment-{len(self.calls)}")


class FakeResolver:
    def __init__(self):
        self.paths = []

    def prepare(self, url):
        path = Path(f"/tmp/{len(self.paths)}.jpg")
        self.paths.append((url, path))
        return path

    def cleanup(self):
        pass


class InstagramBridgeTest(unittest.TestCase):
    def setUp(self):
        self.client = FakeInstagramClient()
        self.resolver = FakeResolver()
        self.bridge = InstagramBridge(
            session_id="session-value",
            client_factory=lambda: self.client,
            resolver=self.resolver,
        )

    def test_posts_single_photo_with_caption(self):
        results = self.bridge.post(
            [
                {
                    "id": "post-1",
                    "message": "Hello Instagram",
                    "media": [{"path": "https://postiz.example/uploads/photo.png"}],
                    "settings": {"post_type": "post"},
                }
            ]
        )

        self.assertEqual(
            results,
            [
                {
                    "id": "post-1",
                    "postId": "photo-pk",
                    "releaseURL": "https://www.instagram.com/p/PHOTO123/",
                    "status": "posted",
                }
            ],
        )
        self.assertEqual(
            self.client.calls,
            [
                ("login", "session-value"),
                ("photo_upload", Path("/tmp/0.jpg"), "Hello Instagram"),
            ],
        )

    def test_posts_carousel_and_followup_comments(self):
        results = self.bridge.post(
            [
                {
                    "id": "post-1",
                    "message": "Carousel caption",
                    "media": [
                        {"path": "https://postiz.example/uploads/one.png"},
                        {"path": "https://postiz.example/uploads/two.png"},
                    ],
                    "settings": {"post_type": "post"},
                },
                {"id": "comment-1", "message": "First comment", "media": []},
            ]
        )

        self.assertEqual(results[0]["postId"], "album-pk")
        self.assertEqual(results[1]["id"], "comment-1")
        self.assertEqual(results[1]["releaseURL"], "https://www.instagram.com/p/ALBUM123/")
        self.assertIn(
            (
                "album_upload",
                [Path("/tmp/0.jpg"), Path("/tmp/1.jpg")],
                "Carousel caption",
            ),
            self.client.calls,
        )
        self.assertIn(("media_comment", "album-pk", "First comment"), self.client.calls)

    def test_dry_run_does_not_login_or_upload(self):
        results = self.bridge.post(
            [
                {
                    "id": "post-1",
                    "message": "Preview",
                    "media": [{"path": "https://postiz.example/uploads/photo.png"}],
                    "settings": {"post_type": "post"},
                }
            ],
            dry_run=True,
        )

        self.assertEqual(results[0]["postId"], "dry-run")
        self.assertEqual(results[0]["status"], "posted")
        self.assertEqual(self.client.calls, [])

    def test_rejects_missing_media(self):
        with self.assertRaisesRegex(BridgeError, "at least one image"):
            self.bridge.post([{"id": "post-1", "message": "No image", "media": []}])

    def test_rejects_story_posts(self):
        with self.assertRaisesRegex(BridgeError, "feed posts"):
            self.bridge.post(
                [
                    {
                        "id": "post-1",
                        "message": "Story",
                        "media": [{"path": "https://postiz.example/uploads/photo.png"}],
                        "settings": {"post_type": "story"},
                    }
                ]
            )

    def test_rejects_unconfigured_session(self):
        bridge = InstagramBridge(
            session_id="",
            client_factory=lambda: self.client,
            resolver=self.resolver,
        )

        with self.assertRaisesRegex(BridgeError, "session ID is not configured"):
            bridge.post(
                [
                    {
                        "id": "post-1",
                        "message": "Post",
                        "media": [{"path": "https://postiz.example/uploads/photo.png"}],
                    }
                ]
            )

    def test_reuses_the_authenticated_client_between_posts(self):
        created_clients = []

        def factory():
            client = FakeInstagramClient()
            created_clients.append(client)
            return client

        bridge = InstagramBridge(
            session_id="session-value",
            client_factory=factory,
            resolver=self.resolver,
        )
        post = [
            {
                "id": "post-1",
                "message": "Caption",
                "media": [{"path": "https://postiz.example/uploads/photo.png"}],
            }
        ]

        bridge.post(post)
        bridge.post(post)

        self.assertEqual(len(created_clients), 1)
        login_calls = [call for call in created_clients[0].calls if call[0] == "login"]
        self.assertEqual(login_calls, [("login", "session-value")])


class MediaResolverTest(unittest.TestCase):
    def test_normalizes_png_to_jpeg_and_cleans_up(self):
        png = base64.b64decode(
            "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
        )
        with tempfile.TemporaryDirectory() as directory:
            upload = Path(directory, "photo.png")
            upload.write_bytes(png)
            resolver = MediaResolver(
                upload_root=Path(directory),
                media_base_url="https://postiz.example/uploads/",
            )

            prepared = resolver.prepare("https://postiz.example/uploads/photo.png")
            self.assertEqual(prepared.read_bytes()[:3], b"\xff\xd8\xff")

            resolver.cleanup()
            self.assertFalse(prepared.exists())

    def test_maps_allowed_postiz_url_to_upload_root(self):
        with tempfile.TemporaryDirectory() as directory:
            upload = Path(directory, "2026", "photo.png")
            upload.parent.mkdir(parents=True)
            upload.write_bytes(b"image")
            resolver = MediaResolver(
                upload_root=Path(directory),
                media_base_url="https://postiz.example/uploads/",
            )

            self.assertEqual(
                resolver.source_path("https://postiz.example/uploads/2026/photo.png"),
                upload,
            )

    def test_rejects_media_from_another_host(self):
        resolver = MediaResolver(
            upload_root=Path("/uploads"),
            media_base_url="https://postiz.example/uploads/",
        )

        with self.assertRaisesRegex(BridgeError, "outside the configured Postiz upload URL"):
            resolver.source_path("https://attacker.example/uploads/photo.png")

    def test_rejects_path_traversal(self):
        resolver = MediaResolver(
            upload_root=Path("/uploads"),
            media_base_url="https://postiz.example/uploads/",
        )

        with self.assertRaisesRegex(BridgeError, "outside the upload directory"):
            resolver.source_path("https://postiz.example/uploads/../secret")


if __name__ == "__main__":
    unittest.main()
