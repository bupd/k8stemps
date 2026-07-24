package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDryRunDoesNotCallLinkedIn(t *testing.T) {
	client := &linkedinClient{liAt: "li-at", jsessionID: "csrf"}
	s := &server{linkedin: client, dryRun: true}
	req := httptest.NewRequest(http.MethodPost, "/post", strings.NewReader(`{
		"posts":[{"id":"post-1","message":"hello","media":[{"path":"/uploads/image.png"}]}]
	}`))
	rec := httptest.NewRecorder()

	s.handlePost(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response postResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 || response.Results[0].PostID != "dry-run" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestPublishTextAndImagePersistsRotatedCookies(t *testing.T) {
	var upload []byte
	var published map[string]any
	mux := http.NewServeMux()
	testServer := httptest.NewServer(mux)
	defer testServer.Close()

	mux.HandleFunc("/voyager/api/voyagerVideoDashMediaUploadMetadata", func(w http.ResponseWriter, r *http.Request) {
		assertSessionHeaders(t, r, "old-li-at", "old-csrf")
		http.SetCookie(w, &http.Cookie{Name: "JSESSIONID", Value: `"new-csrf"`, Path: "/"})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"value": map[string]any{
					"singleUploadUrl":     testServer.URL + "/upload",
					"singleUploadHeaders": map[string]string{"media-type-family": "STILLIMAGE"},
					"urn":                 "urn:li:digitalmediaAsset:image-1",
				},
			},
		})
	})
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		upload, _ = ioReadAll(r)
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/voyager/api/contentcreation/normShares", func(w http.ResponseWriter, r *http.Request) {
		assertSessionHeaders(t, r, "old-li-at", "new-csrf")
		if err := json.NewDecoder(r.Body).Decode(&published); err != nil {
			t.Fatal(err)
		}
		http.SetCookie(w, &http.Cookie{Name: "li_at", Value: "new-li-at", Path: "/"})
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"activity":"urn:li:activity:12345"}}`))
	})

	stateDir := t.TempDir()
	client := &linkedinClient{
		baseURL:    testServer.URL,
		httpClient: testServer.Client(),
		statePath:  filepath.Join(stateDir, "cookies.json"),
		lockPath:   filepath.Join(stateDir, "session.lock"),
		liAt:       "old-li-at",
		jsessionID: "old-csrf",
		userAgent:  "test-agent",
	}
	image := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(image, []byte("\x89PNG\r\n\x1a\nimage"), 0o600); err != nil {
		t.Fatal(err)
	}

	postID, err := client.publish(t.Context(), "hello", []media{{Path: image}})
	if err != nil {
		t.Fatal(err)
	}
	if postID != "urn:li:activity:12345" {
		t.Fatalf("postID = %q", postID)
	}
	if string(upload) != "\x89PNG\r\n\x1a\nimage" {
		t.Fatalf("upload = %q", upload)
	}
	mediaItems := published["media"].([]any)
	if len(mediaItems) != 1 {
		t.Fatalf("media = %#v", mediaItems)
	}

	state := client.loadState()
	if state.LiAt != "new-li-at" || state.JsessionID != "new-csrf" {
		t.Fatalf("state = %#v", state)
	}
}

func TestRejectsTooManyImages(t *testing.T) {
	posts := []postItem{{ID: "post-1", Message: "hello"}}
	for i := 0; i < maxImages+1; i++ {
		posts[0].Media = append(posts[0].Media, media{Path: "/tmp/image.png"})
	}
	body, _ := json.Marshal(postRequest{Posts: posts})
	s := &server{linkedin: &linkedinClient{liAt: "li-at", jsessionID: "csrf"}}
	rec := httptest.NewRecorder()

	s.handlePost(rec, httptest.NewRequest(http.MethodPost, "/post", strings.NewReader(string(body))))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func assertSessionHeaders(t *testing.T, r *http.Request, liAt, csrf string) {
	t.Helper()
	if got := r.Header.Get("Csrf-Token"); got != csrf {
		t.Fatalf("csrf-token = %q", got)
	}
	wantCookie := "li_at=" + liAt + `; JSESSIONID="` + csrf + `"`
	if got := r.Header.Get("Cookie"); got != wantCookie {
		t.Fatalf("cookie = %q, want %q", got, wantCookie)
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
