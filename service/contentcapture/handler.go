package contentcapture

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const maxCaptureRequestBytes = 64 << 10

type Handler struct {
	enabled  bool
	secret   string
	capturer Capturer
}

type readinessProbe interface {
	Ready(context.Context) error
}

func NewHandler(enabled bool, secret string, capturer Capturer) *Handler {
	return &Handler{enabled: enabled, secret: secret, capturer: capturer}
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path == "/health/ready" {
		handler.serveReadiness(response, request)
		return
	}
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		handler.write(response, http.StatusMethodNotAllowed, nil)
		return
	}
	if !handler.authorized(request.Header.Get("Authorization")) {
		handler.write(response, http.StatusUnauthorized, nil)
		return
	}
	if !handler.enabled || handler.capturer == nil {
		handler.write(response, http.StatusFailedDependency, nil)
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxCaptureRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input struct {
		ArticleURL string   `json:"article_url"`
		VideoIDs   []string `json:"video_ids"`
	}
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || !validArticleURL(input.ArticleURL) || !validVideoIDs(input.VideoIDs) {
		handler.write(response, http.StatusBadRequest, nil)
		return
	}
	videos, err := handler.capturer.Capture(request.Context(), input.ArticleURL, input.VideoIDs)
	if err != nil {
		switch {
		case IsCode(err, "disabled"):
			handler.write(response, http.StatusFailedDependency, nil)
		case IsCode(err, "invalid_request"):
			handler.write(response, http.StatusBadRequest, nil)
		case IsCode(err, "missing_video"):
			handler.write(response, http.StatusUnprocessableEntity, nil)
		default:
			handler.write(response, http.StatusServiceUnavailable, nil)
		}
		return
	}
	if len(videos) != len(input.VideoIDs) {
		handler.write(response, http.StatusUnprocessableEntity, nil)
		return
	}
	handler.write(response, http.StatusOK, videos)
}

func (handler *Handler) serveReadiness(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		handler.writeReady(response, http.StatusMethodNotAllowed, false)
		return
	}
	if !handler.authorized(request.Header.Get("Authorization")) {
		handler.writeReady(response, http.StatusUnauthorized, false)
		return
	}
	probe, ok := handler.capturer.(readinessProbe)
	if !handler.enabled || !ok || probe.Ready(request.Context()) != nil {
		handler.writeReady(response, http.StatusServiceUnavailable, false)
		return
	}
	handler.writeReady(response, http.StatusOK, true)
}

func (handler *Handler) authorized(raw string) bool {
	if len(handler.secret) < 32 || !strings.HasPrefix(raw, "Bearer ") {
		return false
	}
	candidate := strings.TrimPrefix(raw, "Bearer ")
	if len(candidate) != len(handler.secret) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(handler.secret)) == 1
}

func (handler *Handler) write(response http.ResponseWriter, status int, videos []Video) {
	if videos == nil {
		videos = []Video{}
	}
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(struct {
		Videos []Video `json:"videos"`
	}{Videos: videos})
}

func (handler *Handler) writeReady(response http.ResponseWriter, status int, ready bool) {
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(struct {
		Ready bool `json:"ready"`
	}{Ready: ready})
}
