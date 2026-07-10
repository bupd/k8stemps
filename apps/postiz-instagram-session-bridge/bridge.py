import os
import tempfile
from pathlib import Path
from threading import Lock
from urllib.parse import unquote, urlparse


class BridgeError(Exception):
    pass


class MediaResolver:
    def __init__(self, upload_root, media_base_url, max_media_bytes=20 << 20):
        self.upload_root = Path(upload_root).resolve()
        self.media_base_url = media_base_url
        self.max_media_bytes = max_media_bytes
        self._temporary_paths = []

    def source_path(self, media_url):
        base = urlparse(self.media_base_url)
        candidate = urlparse(media_url)
        base_path = base.path.rstrip("/") + "/"
        candidate_path = unquote(candidate.path)
        if (
            candidate.scheme != base.scheme
            or candidate.netloc != base.netloc
            or not candidate_path.startswith(base_path)
        ):
            raise BridgeError("media URL is outside the configured Postiz upload URL")

        relative = Path(candidate_path.removeprefix(base_path))
        source = (self.upload_root / relative).resolve()
        if not source.is_relative_to(self.upload_root):
            raise BridgeError("media path is outside the upload directory")
        if not source.is_file():
            raise BridgeError("media file does not exist in the Postiz upload directory")
        if source.stat().st_size > self.max_media_bytes:
            raise BridgeError(f"media file exceeds {self.max_media_bytes} bytes")
        return source

    def prepare(self, media_url):
        from PIL import Image, ImageOps

        source = self.source_path(media_url)
        with Image.open(source) as image:
            image = ImageOps.exif_transpose(image).convert("RGB")
            temporary = tempfile.NamedTemporaryFile(
                prefix="postiz-instagram-",
                suffix=".jpg",
                delete=False,
            )
            temporary.close()
            image.save(temporary.name, format="JPEG", quality=95, optimize=True)

        path = Path(temporary.name)
        self._temporary_paths.append(path)
        return path

    def cleanup(self):
        for path in self._temporary_paths:
            path.unlink(missing_ok=True)
        self._temporary_paths.clear()


class InstagramBridge:
    def __init__(self, session_id, client_factory, resolver):
        self.session_id = session_id.strip()
        self.client_factory = client_factory
        self.resolver = resolver
        self._lock = Lock()
        self._client = None

    def post(self, posts, dry_run=False):
        if not posts:
            raise BridgeError("missing posts")

        first = posts[0]
        media = first.get("media") or []
        if not media:
            raise BridgeError("Instagram requires at least one image")
        if len(media) > 10:
            raise BridgeError("Instagram supports at most 10 carousel images")
        if (first.get("settings") or {}).get("post_type", "post") != "post":
            raise BridgeError("the session bridge currently supports feed posts only")

        if dry_run:
            return [
                {
                    "id": first.get("id", "single"),
                    "postId": "dry-run",
                    "releaseURL": "https://www.instagram.com/",
                    "status": "posted",
                }
            ]
        if not self.session_id:
            raise BridgeError("Instagram session ID is not configured")

        with self._lock:
            return self._post_locked(posts, media)

    def _post_locked(self, posts, media):
        try:
            paths = [self.resolver.prepare(item.get("path", "")) for item in media]
            client = self._authenticated_client()

            caption = (posts[0].get("message") or "").strip()
            if len(paths) == 1:
                uploaded = client.photo_upload(paths[0], caption)
            else:
                uploaded = client.album_upload(paths, caption)

            release_url = f"https://www.instagram.com/p/{uploaded.code}/"
            results = [
                {
                    "id": posts[0].get("id", "single"),
                    "postId": str(uploaded.pk),
                    "releaseURL": release_url,
                    "status": "posted",
                }
            ]
            for post in posts[1:]:
                message = (post.get("message") or "").strip()
                if not message:
                    continue
                comment = client.media_comment(uploaded.pk, message)
                results.append(
                    {
                        "id": post.get("id", "comment"),
                        "postId": str(comment.pk),
                        "releaseURL": release_url,
                        "status": "posted",
                    }
                )
            return results
        finally:
            self.resolver.cleanup()

    def _authenticated_client(self):
        if self._client is not None:
            return self._client
        client = self.client_factory()
        if not client.login_by_sessionid(self.session_id):
            raise BridgeError("Instagram rejected the configured session ID")
        self._client = client
        return client


def instagram_client():
    from instagrapi import Client

    return Client()


def bridge_from_environment():
    resolver = MediaResolver(
        upload_root=os.environ.get("UPLOAD_ROOT", "/uploads"),
        media_base_url=os.environ.get(
            "MEDIA_BASE_URL", "https://postiz.bupd.xyz/uploads/"
        ),
    )
    return InstagramBridge(
        session_id=os.environ.get("INSTAGRAM_SESSION_ID", ""),
        client_factory=instagram_client,
        resolver=resolver,
    )
