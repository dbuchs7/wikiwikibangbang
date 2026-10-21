package dest

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// webdav is a folder on a WebDAV server over HTTPS, e.g. a NAS, Nextcloud
// or a file server. The user is in the address or UsernameEnv, the
// password in PasswordEnv. The folder must exist.
type webdav struct {
	base       *url.URL // the folder, ending in a slash
	user, pass string
	client     *http.Client
}

func newWebDAV(a address, env Env) (Target, error) {
	cfg, err := tlsConfig(env, a.url.Hostname())
	if err != nil {
		return nil, err
	}
	base := *a.url
	user := base.User.Username()
	if user == "" {
		user = env(UsernameEnv)
	}
	base.User, base.RawQuery, base.Fragment = nil, "", ""
	base.Path = strings.TrimSuffix(a.dir, "/") + "/"
	base.RawPath = ""
	return &webdav{base: &base, user: user, pass: env(PasswordEnv), client: httpClient(cfg)}, nil
}

func (w *webdav) url(name string) string {
	u := *w.base
	u.Path += name
	return u.String()
}

func (w *webdav) do(ctx context.Context, method, target string, body io.Reader, header map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if w.user != "" || w.pass != "" {
		req.SetBasicAuth(w.user, w.pass)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return w.client.Do(req)
}

func (w *webdav) Put(ctx context.Context, name string, r io.Reader) error {
	if err := checkName(name); err != nil {
		return err
	}
	part := name + partSuffix
	resp, err := w.do(ctx, http.MethodPut, w.url(part), readerCtx{ctx, r}, map[string]string{"Content-Type": "application/octet-stream"})
	if err != nil {
		return fmt.Errorf("webdav: storing %s: %w", name, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return statusError("webdav", "PUT", part, resp)
	}
	resp, err = w.do(ctx, "MOVE", w.url(part), nil, map[string]string{"Destination": w.url(name), "Overwrite": "T"})
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
			err = statusError("webdav", "MOVE", part, resp)
		}
	}
	if err != nil {
		if resp, derr := w.do(context.WithoutCancel(ctx), http.MethodDelete, w.url(part), nil, nil); derr == nil {
			resp.Body.Close()
		}
		return err
	}
	return nil
}

const propfind = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`

type multistatus struct {
	Responses []struct {
		Href       string    `xml:"DAV: href"`
		Collection *struct{} `xml:"DAV: propstat>prop>resourcetype>collection"`
	} `xml:"DAV: response"`
}

func (w *webdav) List(ctx context.Context) ([]string, error) {
	resp, err := w.do(ctx, "PROPFIND", w.base.String(), strings.NewReader(propfind), map[string]string{"Depth": "1", "Content-Type": "application/xml"})
	if err != nil {
		return nil, fmt.Errorf("webdav: listing the folder: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, statusError("webdav", "PROPFIND", w.base.Path, resp)
	}
	var ms multistatus
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxListing)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("webdav: reading the listing: %w", err)
	}
	var out []string
	for _, r := range ms.Responses {
		if r.Collection != nil {
			continue
		}
		u, err := url.Parse(r.Href)
		if err != nil {
			continue
		}
		p := u.Path
		if strings.TrimSuffix(p, "/") == strings.TrimSuffix(w.base.Path, "/") {
			continue
		}
		if name := path.Base(p); checkName(name) == nil {
			out = append(out, name)
		}
	}
	return out, nil
}

func (w *webdav) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	resp, err := w.do(ctx, http.MethodGet, w.url(name), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("webdav: reading %s: %w", name, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, statusError("webdav", "GET", name, resp)
	}
	return resp.Body, nil
}

func (w *webdav) Delete(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	resp, err := w.do(ctx, http.MethodDelete, w.url(name), nil, nil)
	if err != nil {
		return fmt.Errorf("webdav: deleting %s: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return statusError("webdav", "DELETE", name, resp)
	}
	return nil
}
