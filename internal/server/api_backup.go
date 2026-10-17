package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/dbuchs7/wikiwikibangbang/internal/audit"
	"github.com/dbuchs7/wikiwikibangbang/internal/backup"
	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
)

// backup streams an encrypted backup of the data directory (ADR-0016,
// ADR-0034). The passphrase travels in the body of a POST, never in a URL,
// where logs and browser history would keep it.
func (h authHandlers) backup(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
	if !h.isAdmin(p) {
		writeError(w, http.StatusForbidden, "administrators only")
		return
	}
	var req struct {
		Passphrase string `json:"passphrase"`
	}
	if err := readJSON(w, r, &req); err != nil {
		badRequest(w, err)
		return
	}
	if len([]rune(req.Passphrase)) < backup.MinPassphrase {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("a backup passphrase has at least %d characters", backup.MinPassphrase))
		return
	}
	files, err := h.d.DB.Snapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the snapshot failed")
		return
	}
	now := time.Now().UTC()
	h.record(audit.Entry{Event: audit.BackupCreated, Actor: string(p.User), Addr: clientAddr(r)})
	hd := w.Header()
	hd.Set("Content-Type", "application/octet-stream")
	hd.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="wikiwikibangbang-%s.wwbb"`, now.Format("2006-01-02T1504Z")))
	hd.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// A large backup outlasts the server's write timeout; each chunk
	// extends the deadline instead.
	rc := http.NewResponseController(w)
	out := writerFunc(func(b []byte) (int, error) {
		_ = rc.SetWriteDeadline(time.Now().Add(60 * time.Second))
		return w.Write(b)
	})
	if err := backup.Write(out, req.Passphrase, files, buildinfo.Get().String(), now); err != nil {
		// The status is sent; cutting the connection makes the client see
		// a truncated download, which decryption refuses.
		panic(http.ErrAbortHandler)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }
