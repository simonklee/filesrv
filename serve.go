// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package filesrv

import (
	"net/http"
	"path"
	"strings"
)

func serveFile(w http.ResponseWriter, r *http.Request, fs http.FileSystem, name string) {
	f, err := fs.Open(name)

	if err != nil {
		http.NotFound(w, r)
		return
	}

	defer f.Close()
	d, err := f.Stat()

	if err != nil {
		http.NotFound(w, r)
		return
	}

	if _, haveType := w.Header()["Content-Type"]; !haveType {
		ff, ok := f.(*file)

		if ok && ff.fi.contentType != "" {
			w.Header().Set("Content-Type", ff.fi.contentType)
		}
	}

	if _, haveETag := w.Header()["ETag"]; !haveETag {
		ff, ok := f.(*file)

		if ok && ff.fi.etag != "" {
			w.Header().Set("ETag", ff.fi.etag)
		}
	}

	// serveContent will check modification time
	http.ServeContent(w, r, d.Name(), d.ModTime(), f)
}

type fileHandler struct {
	root http.FileSystem
}

// FileServer returns a handler that serves HTTP requests
// with the contents of the file system rooted at root.
//
// To use the operating system's file system implementation,
// use http.Dir:
//
//     http.Handle("/", http.FileServer(http.Dir("/tmp")))
func FileServer(root http.FileSystem) http.Handler {
	return &fileHandler{root}
}

func (f *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upath := r.URL.Path

	if !strings.HasPrefix(upath, "/") {
		upath = "/" + upath
		r.URL.Path = upath
	}

	if q := r.URL.RawQuery; q != "" {
		upath += "?" + q
	}

	serveFile(w, r, f.root, path.Clean(upath))
}
