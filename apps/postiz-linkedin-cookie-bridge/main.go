package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxRequestBytes = 2 << 20
	maxMediaBytes   = 20 << 20
	maxImages       = 9
)

var activityPattern = regexp.MustCompile(`urn:li:activity:([0-9]+)`)

type server struct {
	logger      *slog.Logger
	linkedin    *linkedinClient
	bearerToken string
	dryRun      bool
}

type linkedinClient struct {
	baseURL    string
	httpClient *http.Client
	statePath  string
	lockPath   string
	liAt       string
	jsessionID string
	userAgent  string
	mu         sync.Mutex
}

type sessionState struct {
	LiAt       string `json:"li_at"`
	JsessionID string `json:"jsession_id"`
}

type postRequest struct {
	Posts   []postItem `json:"posts"`
	Message string     `json:"message"`
	Media   []media    `json:"media"`
	DryRun  *bool      `json:"dryRun"`
}

type postItem struct {
	ID      string  `json:"id"`
	Message string  `json:"message"`
	Media   []media `json:"media"`
}

type media struct {
	Path string `json:"path"`
	Alt  string `json:"alt"`
}

type postResponse struct {
	Results []postResult `json:"results"`
}

type postResult struct {
	ID         string `json:"id"`
	PostID     string `json:"postId"`
	ReleaseURL string `json:"releaseURL"`
	Status     string `json:"status"`
}

type uploadedMedia struct {
	Category string `json:"category"`
	MediaURN string `json:"mediaUrn"`
	Targets  []any  `json:"tapTargets"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	client, err := newLinkedinClient()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	s := &server{
		logger:      logger,
		linkedin:    client,
		bearerToken: strings.TrimSpace(os.Getenv("POSTIZ_LINKEDIN_BRIDGE_TOKEN")),
		dryRun:      envBool("DRY_RUN"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("POST /post", s.handlePost)

	addr := ":" + envString("PORT", "8080")
	logger.Info("starting Postiz LinkedIn bridge", "addr", addr, "dryRun", s.dryRun)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func newLinkedinClient() (*linkedinClient, error) {
	liAt := strings.TrimSpace(os.Getenv("LINKEDIN_LI_AT"))
	jsessionID := trimCookieQuotes(os.Getenv("LINKEDIN_JSESSION_ID"))
	if liAt == "" || jsessionID == "" {
		return nil, errors.New("LINKEDIN_LI_AT and LINKEDIN_JSESSION_ID are required")
	}

	timeout, err := time.ParseDuration(envString("LINKEDIN_TIMEOUT", "90s"))
	if err != nil {
		return nil, fmt.Errorf("parse LINKEDIN_TIMEOUT: %w", err)
	}

	stateDir := envString("STATE_DIRECTORY", "/state")
	return &linkedinClient{
		baseURL:    strings.TrimRight(envString("LINKEDIN_BASE_URL", "https://www.linkedin.com"), "/"),
		httpClient: &http.Client{Timeout: timeout},
		statePath:  filepath.Join(stateDir, "cookies.json"),
		lockPath:   filepath.Join(stateDir, "session.lock"),
		liAt:       liAt,
		jsessionID: jsessionID,
		userAgent:  envString("LINKEDIN_USER_AGENT", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36"),
	}, nil
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.linkedin.liAt == "" || s.linkedin.jsessionID == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "credentials missing"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *server) handlePost(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	defer r.Body.Close()
	var req postRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBytes)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	posts := req.normalizedPosts()
	if len(posts) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing posts"})
		return
	}

	message := joinMessages(posts)
	if message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing message"})
		return
	}
	if len([]rune(message)) > 3000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "message exceeds LinkedIn's 3000 character limit"})
		return
	}

	dryRun := s.dryRun
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}
	if dryRun {
		results := resultsFor(posts, "dry-run", "https://www.linkedin.com", "posted")
		writeJSON(w, http.StatusOK, postResponse{Results: results})
		return
	}

	allMedia := flattenMedia(posts)
	if len(allMedia) > maxImages {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "LinkedIn accepts at most 9 images per post"})
		return
	}

	postID, err := s.linkedin.publish(r.Context(), message, allMedia)
	if err != nil {
		s.logger.Error("LinkedIn publish failed", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	releaseURL := "https://www.linkedin.com/feed/update/" + postID + "/"
	s.logger.Info("posted to LinkedIn", "postID", postID, "items", len(posts), "media", len(allMedia))
	writeJSON(w, http.StatusOK, postResponse{
		Results: resultsFor(posts, postID, releaseURL, "posted"),
	})
}

func (s *server) authorized(r *http.Request) bool {
	return s.bearerToken == "" || r.Header.Get("Authorization") == "Bearer "+s.bearerToken
}

func (c *linkedinClient) publish(ctx context.Context, message string, mediaItems []media) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(c.statePath), 0o700); err != nil {
		return "", fmt.Errorf("create state directory: %w", err)
	}
	lock, err := os.OpenFile(c.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open session lock: %w", err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock LinkedIn session: %w", err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck

	state := c.loadState()
	uploads := make([]uploadedMedia, 0, len(mediaItems))
	for _, item := range mediaItems {
		data, filename, contentType, err := readMedia(ctx, item.Path)
		if err != nil {
			return "", err
		}
		upload, err := c.uploadImage(ctx, &state, filename, contentType, data)
		if err != nil {
			return "", err
		}
		uploads = append(uploads, upload)
	}

	payload := map[string]any{
		"visibleToConnectionsOnly":  false,
		"externalAudienceProviders": []any{},
		"commentaryV2": map[string]any{
			"text":       message,
			"attributes": []any{},
		},
		"origin":                 "FEED",
		"allowedCommentersScope": "ALL",
		"postState":              "PUBLISHED",
		"media":                  uploads,
	}

	body, status, err := c.doJSON(ctx, &state, http.MethodPost, c.baseURL+"/voyager/api/contentcreation/normShares", payload)
	if err != nil {
		return "", fmt.Errorf("create LinkedIn post: %w", err)
	}
	if status < 200 || status > 299 {
		return "", fmt.Errorf("create LinkedIn post: LinkedIn returned HTTP %d", status)
	}
	match := activityPattern.FindSubmatch(body)
	if len(match) != 2 {
		return "", errors.New("create LinkedIn post: response did not contain an activity URN")
	}
	return "urn:li:activity:" + string(match[1]), nil
}

func (c *linkedinClient) uploadImage(ctx context.Context, state *sessionState, filename, contentType string, data []byte) (uploadedMedia, error) {
	payload := map[string]any{
		"mediaUploadType": "IMAGE_SHARING",
		"fileSize":        len(data),
		"filename":        filename,
	}
	body, status, err := c.doJSON(ctx, state, http.MethodPost, c.baseURL+"/voyager/api/voyagerVideoDashMediaUploadMetadata?action=upload", payload)
	if err != nil {
		return uploadedMedia{}, fmt.Errorf("register LinkedIn image: %w", err)
	}
	if status < 200 || status > 299 {
		return uploadedMedia{}, fmt.Errorf("register LinkedIn image: LinkedIn returned HTTP %d", status)
	}

	var response struct {
		Data struct {
			Value struct {
				SingleUploadURL     string            `json:"singleUploadUrl"`
				SingleUploadHeaders map[string]string `json:"singleUploadHeaders"`
				URN                 string            `json:"urn"`
			} `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return uploadedMedia{}, fmt.Errorf("decode LinkedIn upload registration: %w", err)
	}
	value := response.Data.Value
	if value.SingleUploadURL == "" || value.URN == "" {
		return uploadedMedia{}, errors.New("LinkedIn upload registration omitted URL or media URN")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, value.SingleUploadURL, bytes.NewReader(data))
	if err != nil {
		return uploadedMedia{}, fmt.Errorf("create LinkedIn image upload: %w", err)
	}
	for key, value := range value.SingleUploadHeaders {
		req.Header.Set(key, value)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("User-Agent", c.userAgent)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return uploadedMedia{}, fmt.Errorf("upload LinkedIn image: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	c.updateStateFromResponse(state, resp)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return uploadedMedia{}, fmt.Errorf("upload LinkedIn image: LinkedIn returned HTTP %d", resp.StatusCode)
	}

	return uploadedMedia{Category: "IMAGE", MediaURN: value.URN, Targets: []any{}}, nil
}

func (c *linkedinClient) doJSON(ctx context.Context, state *sessionState, method, endpoint string, payload any) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.linkedin.normalized+json+2.1")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("Csrf-Token", state.JsessionID)
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.baseURL+"/feed/")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-Li-Lang", "en_US")
	req.Header.Set("X-RestLi-Protocol-Version", "2.0.0")
	req.Header.Set("Cookie", cookieHeader(*state))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	c.updateStateFromResponse(state, resp)
	return responseBody, resp.StatusCode, nil
}

func (c *linkedinClient) loadState() sessionState {
	state := sessionState{LiAt: c.liAt, JsessionID: c.jsessionID}
	data, err := os.ReadFile(c.statePath)
	if err == nil {
		var saved sessionState
		if json.Unmarshal(data, &saved) == nil && saved.LiAt != "" && saved.JsessionID != "" {
			state = saved
		}
	}
	return state
}

func (c *linkedinClient) updateStateFromResponse(state *sessionState, resp *http.Response) {
	changed := false
	for _, cookie := range resp.Cookies() {
		switch cookie.Name {
		case "li_at":
			if cookie.Value != "" && cookie.Value != state.LiAt {
				state.LiAt = cookie.Value
				changed = true
			}
		case "JSESSIONID":
			value := trimCookieQuotes(cookie.Value)
			if value != "" && value != state.JsessionID {
				state.JsessionID = value
				changed = true
			}
		}
	}
	if !changed {
		return
	}
	data, err := json.Marshal(state)
	if err != nil {
		return
	}
	tmp := c.statePath + ".tmp." + strconv.Itoa(os.Getpid())
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, c.statePath)
	}
}

func readMedia(ctx context.Context, path string) ([]byte, string, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", "", errors.New("empty media path")
	}
	parsed, err := url.Parse(path)
	if err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, "", "", fmt.Errorf("create media download: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, "", "", fmt.Errorf("download media: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, "", "", fmt.Errorf("download media: HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
		if err != nil {
			return nil, "", "", fmt.Errorf("read media: %w", err)
		}
		if len(data) > maxMediaBytes {
			return nil, "", "", errors.New("media exceeds 20 MiB limit")
		}
		filename := filepath.Base(parsed.Path)
		if filename == "." || filename == "/" || filename == "" {
			filename = "image"
		}
		contentType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		return validateImage(data, filename, contentType)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", "", fmt.Errorf("read media: %w", err)
	}
	if len(data) > maxMediaBytes {
		return nil, "", "", errors.New("media exceeds 20 MiB limit")
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return validateImage(data, filepath.Base(path), contentType)
}

func validateImage(data []byte, filename, contentType string) ([]byte, string, string, error) {
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", "", fmt.Errorf("unsupported LinkedIn media type %q; only images are supported", contentType)
	}
	if filename == "" {
		filename = "image"
	}
	return data, filename, contentType, nil
}

func (r postRequest) normalizedPosts() []postItem {
	if len(r.Posts) > 0 {
		return r.Posts
	}
	if strings.TrimSpace(r.Message) == "" && len(r.Media) == 0 {
		return nil
	}
	return []postItem{{Message: r.Message, Media: r.Media}}
}

func joinMessages(posts []postItem) string {
	messages := make([]string, 0, len(posts))
	for _, post := range posts {
		if message := strings.TrimSpace(post.Message); message != "" {
			messages = append(messages, message)
		}
	}
	return strings.Join(messages, "\n\n")
}

func flattenMedia(posts []postItem) []media {
	var result []media
	for _, post := range posts {
		for _, item := range post.Media {
			if strings.TrimSpace(item.Path) != "" {
				result = append(result, item)
			}
		}
	}
	return result
}

func resultsFor(posts []postItem, postID, releaseURL, status string) []postResult {
	results := make([]postResult, 0, len(posts))
	for _, post := range posts {
		results = append(results, postResult{
			ID:         post.ID,
			PostID:     postID,
			ReleaseURL: releaseURL,
			Status:     status,
		})
	}
	return results
}

func cookieHeader(state sessionState) string {
	return "li_at=" + state.LiAt + `; JSESSIONID="` + state.JsessionID + `"`
}

func trimCookieQuotes(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	value, _ := strconv.ParseBool(strings.TrimSpace(os.Getenv(name)))
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
