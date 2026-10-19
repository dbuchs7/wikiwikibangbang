package server

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/dbuchs7/wikiwikibangbang/internal/auth"
	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
)

// avatarRoutes are the profile pictures of accounts (ADR-0050).
func avatarRoutes(d Deps) []Route {
	h := authHandlers{d: d}
	return []Route{
		{Method: http.MethodPut, Pattern: "/api/me/avatar", Summary: "Set one's profile picture: the body is a PNG, JPEG, GIF or WebP image (application/octet-stream)", Handler: http.HandlerFunc(h.setAvatar)},
		{Method: http.MethodDelete, Pattern: "/api/me/avatar", Summary: "Remove one's profile picture", Handler: http.HandlerFunc(h.removeAvatar)},
		{Method: http.MethodGet, Pattern: "/api/users/{id}/avatar", Summary: "A user's profile picture; not found when they have none", Handler: http.HandlerFunc(h.avatar)},
	}
}

// avatarTypes are the image formats a profile picture may have: those a
// browser shows without running anything (as inlineTypes, ADR-0045).
var avatarTypes = inlineTypes

// avatarTag names a picture's content, for caching and for the UI to ask
// for a new one when it changes.
func avatarTag(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// setAvatar stores the request body as the caller's profile picture. Like
// an upload, the body must be application/octet-stream, which a cross-site
// form cannot send (ADR-0027); its format is read from its bytes.
func (h authHandlers) setAvatar(w http.ResponseWriter, r *http.Request) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/octet-stream" {
		writeError(w, http.StatusUnsupportedMediaType, "the request body must be application/octet-stream")
		return
	}
	limit := budget.Get().AvatarBytes
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a profile picture may have at most %d bytes", limit))
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, "the picture could not be read")
		return
	case len(data) == 0:
		writeError(w, http.StatusBadRequest, "the picture is empty")
		return
	}
	mediaType := http.DetectContentType(data)
	if !avatarTypes[mediaType] {
		writeError(w, http.StatusBadRequest, "a profile picture must be a PNG, JPEG, GIF or WebP image")
		return
	}
	p, _ := PrincipalFrom(r.Context())
	if err := h.d.DB.Accounts.SetAvatar(p.User, data, mediaType); err != nil {
		writeError(w, http.StatusInternalServerError, "picture not saved")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"avatar": avatarTag(data)})
}

func (h authHandlers) removeAvatar(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if err := h.d.DB.Accounts.SetAvatar(p.User, nil, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "picture not removed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// avatar sends a profile picture to any signed-in caller: who wrote what is
// visible to them already, and the picture belongs with the name. It is
// never run as a page of this site.
func (h authHandlers) avatar(w http.ResponseWriter, r *http.Request) {
	data, mediaType := h.d.DB.Accounts.Avatar(auth.UserID(r.PathValue("id")))
	if data == nil {
		writeError(w, http.StatusNotFound, "no profile picture")
		return
	}
	tag := strconv.Quote(avatarTag(data))
	hd := w.Header()
	hd.Set("Content-Type", mediaType)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hd.Set("Cache-Control", "private, max-age=86400")
	hd.Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hd.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
