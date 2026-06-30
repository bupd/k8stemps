package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxRequestBytes = 2 << 20
const maxMediaBytes = 20 << 20

var xStatusURLPattern = regexp.MustCompile(`https://(?:x|twitter)\.com/(?:i/web/status|[^/\s]+/status)/([0-9]+)`)

type server struct {
	logger       *slog.Logger
	crosspostBin string
	crosspostArg []string
	timeout      time.Duration
	dryRun       bool
	bearerToken  string
}

type postRequest struct {
	Posts   []postItem `json:"posts"`
	Message string     `json:"message"`
	Media   []media    `json:"media"`
	DryRun  *bool      `json:"dryRun"`
}

type postItem struct {
	ID      string         `json:"id"`
	Message string         `json:"message"`
	Media   []media        `json:"media"`
	Raw     map[string]any `json:"-"`
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
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	timeout, err := envDuration("CROSSPOST_TIMEOUT", 2*time.Minute)
	if err != nil {
		logger.Error("invalid timeout", "error", err)
		os.Exit(1)
	}

	s := &server{
		logger:       logger,
		crosspostBin: envString("CROSSPOST_BIN", "crosspost"),
		crosspostArg: strings.Fields(envString("CROSSPOST_FLAGS", "-t")),
		timeout:      timeout,
		dryRun:       envBool("DRY_RUN"),
		bearerToken:  strings.TrimSpace(os.Getenv("POSTIZ_X_BRIDGE_TOKEN")),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /post", s.handlePost)

	addr := ":" + envString("PORT", "8080")
	logger.Info("starting postiz x bridge", "addr", addr, "crosspostBin", s.crosspostBin, "crosspostFlags", s.crosspostArg, "dryRun", s.dryRun)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

	dryRun := s.dryRun
	if req.DryRun != nil {
		dryRun = *req.DryRun
	}

	results := make([]postResult, 0, len(posts))
	for _, post := range posts {
		result, err := s.post(r.Context(), post, dryRun)
		if err != nil {
			s.logger.Error("crosspost failed", "postID", post.ID, "error", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		results = append(results, result)
	}

	writeJSON(w, http.StatusOK, postResponse{Results: results})
}

func (s *server) authorized(r *http.Request) bool {
	if s.bearerToken == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+s.bearerToken
}

func (s *server) post(ctx context.Context, post postItem, dryRun bool) (postResult, error) {
	message := strings.TrimSpace(post.Message)
	if message == "" {
		return postResult{}, errors.New("missing message")
	}

	args := append([]string{}, s.crosspostArg...)
	if len(post.Media) > 0 && strings.TrimSpace(post.Media[0].Path) != "" {
		mediaPath := strings.TrimSpace(post.Media[0].Path)
		cleanup := func() {}
		var err error
		if !dryRun {
			mediaPath, cleanup, err = localMediaPath(ctx, mediaPath)
			if err != nil {
				return postResult{}, err
			}
			defer cleanup()
		}
		args = append(args, "--image", mediaPath)
		if alt := strings.TrimSpace(post.Media[0].Alt); alt != "" {
			args = append(args, "--image-alt", alt)
		}
	}
	args = append(args, message)

	if dryRun {
		return postResult{
			ID:         post.ID,
			PostID:     "dry-run",
			ReleaseURL: "https://x.com",
			Status:     "posted",
			Stdout:     commandPreview(s.crosspostBin, args),
		}, nil
	}

	if strings.TrimSpace(os.Getenv("TWITTER_AUTH_TOKEN")) == "" && strings.TrimSpace(os.Getenv("AUTH_TOKEN")) == "" {
		return postResult{}, errors.New("TWITTER_AUTH_TOKEN is not configured")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, s.crosspostBin, args...)
	cmd.Env = normalizedCrosspostEnv(os.Environ())
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
		return postResult{}, fmt.Errorf("crosspost timed out after %s", s.timeout)
	}
	if err != nil {
		return postResult{}, fmt.Errorf("crosspost exited: %v: %s", err, text)
	}

	releaseURL, postID := xStatusURL(text)
	s.logger.Info("posted to x", "postID", post.ID, "releaseURL", releaseURL)
	return postResult{
		ID:         post.ID,
		PostID:     postID,
		ReleaseURL: releaseURL,
		Status:     "posted",
		Stdout:     text,
	}, nil
}

func localMediaPath(ctx context.Context, path string) (string, func(), error) {
	mediaURL, err := url.Parse(path)
	if err != nil || mediaURL.Scheme == "" {
		return path, func() {}, nil
	}
	if mediaURL.Scheme != "http" && mediaURL.Scheme != "https" {
		return "", nil, fmt.Errorf("unsupported media URL scheme %q", mediaURL.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", nil, fmt.Errorf("create media request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", nil, fmt.Errorf("download media: unexpected status %s", resp.Status)
	}

	ext := filepath.Ext(mediaURL.Path)
	if len(ext) > 16 {
		ext = ""
	}
	file, err := os.CreateTemp("", "postiz-x-media-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("create media temp file: %w", err)
	}

	cleanup := func() {
		if err := os.Remove(file.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("remove media temp file", "path", file.Name(), "error", err)
		}
	}

	written, err := io.Copy(file, io.LimitReader(resp.Body, maxMediaBytes+1))
	closeErr := file.Close()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("write media temp file: %w", err)
	}
	if closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("close media temp file: %w", closeErr)
	}
	if written > maxMediaBytes {
		cleanup()
		return "", nil, fmt.Errorf("media file exceeds %d bytes", maxMediaBytes)
	}

	return file.Name(), cleanup, nil
}

func (r postRequest) normalizedPosts() []postItem {
	if len(r.Posts) > 0 {
		return r.Posts
	}
	if strings.TrimSpace(r.Message) == "" {
		return nil
	}
	return []postItem{{
		ID:      "single",
		Message: r.Message,
		Media:   r.Media,
	}}
}

func normalizedCrosspostEnv(values []string) []string {
	env := envMap(values)
	setDefaultEnv(env, "TWITTER_AUTH_TOKEN", "AUTH_TOKEN")
	setDefaultEnv(env, "AUTH_TOKEN", "TWITTER_AUTH_TOKEN")

	result := make([]string, 0, len(env))
	for key, value := range env {
		result = append(result, key+"="+value)
	}
	return result
}

func envMap(values []string) map[string]string {
	env := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if ok {
			env[key] = val
		}
	}
	return env
}

func setDefaultEnv(env map[string]string, target string, aliases ...string) {
	if strings.TrimSpace(env[target]) != "" {
		return
	}
	for _, alias := range aliases {
		if value := strings.TrimSpace(env[alias]); value != "" {
			env[target] = value
			return
		}
	}
}

func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	return duration, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		slog.Error("write response", "error", err)
	}
}

func commandPreview(name string, args []string) string {
	quoted := []string{name}
	for _, arg := range args {
		quoted = append(quoted, strconv.Quote(arg))
	}
	return strings.Join(quoted, " ")
}

func xStatusURL(output string) (releaseURL string, postID string) {
	match := xStatusURLPattern.FindStringSubmatch(output)
	if len(match) == 2 {
		return match[0], match[1]
	}
	return "https://x.com", "posted"
}
