package web

import (
	"io/fs"
	"testing"
)

func TestEmbeddedUIHasEntryPointAndAssets(t *testing.T) {
	for _, name := range []string{"index.html", "assets/app.js", "assets/style.css"} {
		if _, err := fs.Stat(FS(), name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
