// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package filesrv

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

type fileInfo struct {
	basename string
	modtime  time.Time
	size     int
}

func (f fileInfo) Name() string       { return f.basename }
func (f fileInfo) Sys() interface{}   { return nil }
func (f fileInfo) ModTime() time.Time { return f.modtime }
func (f fileInfo) IsDir() bool        { return false }
func (f fileInfo) Size() int64        { return int64(f.size) }
func (f fileInfo) Mode() os.FileMode {
	if f.IsDir() {
		return 0755 | os.ModeDir
	}
	return 0644
}

type file struct {
	io.ReadSeeker
	fi fileInfo
}

func (f *file) Close() error               { return nil }
func (f *file) Stat() (os.FileInfo, error) { return f.fi, nil }
func (f *file) Readdir(count int) ([]os.FileInfo, error) {
	return nil, io.EOF
}

type remoteFileSystem struct {
	origin string
}

func (fs *remoteFileSystem) Open(name string) (http.File, error) {
	path := fs.origin + name
	res, err := http.DefaultClient.Get(path)

	if err != nil {
		return nil, err
	}

	buf, _ := ioutil.ReadAll(res.Body)
	rd := bytes.NewReader(buf)

	f := &file{
		ReadSeeker: rd,

		fi: fileInfo{
			size:     rd.Len(),
			modtime:  time.Now().UTC(),
			basename: path,
		},
	}
	return f, nil
}

func New(origin string) http.FileSystem {
	return &remoteFileSystem{
		origin: origin,
	}
}

type memoryCacheFilesystem struct {
	fs    http.FileSystem
	cache map[string]http.File
}

func NewCache(fs http.FileSystem) http.FileSystem {
	return &memoryCacheFilesystem{
		fs:    fs,
		cache: make(map[string]http.File),
	}
}

func (fs *memoryCacheFilesystem) Open(name string) (http.File, error) {
	if f, ok := fs.cache[name]; ok {
		return f, nil
	}

	f, err := fs.fs.Open(name)

	if err != nil {
		return nil, err
	}

	fs.cache[name] = f
	return f, nil
}
