// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package filesrv

import (
	"bytes"
	"container/list"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/simonz05/util/log"
)

type fileInfo struct {
	basename    string
	modtime     time.Time
	size        int
	contentType string
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
	fi  fileInfo
	buf []byte
}

func (f *file) Close() error                             { return nil }
func (f *file) Stat() (os.FileInfo, error)               { return f.fi, nil }
func (f *file) Readdir(count int) ([]os.FileInfo, error) { return nil, io.EOF }

// returns a read clone of the file
func (f *file) readClone() http.File {
	if f.buf == nil {
		// todo
		panic("copy a readClone")
	}
	return &file{
		ReadSeeker: bytes.NewReader(f.buf),
		fi:         f.fi,
	}
}

type remoteFileSystem struct {
	origin string
}

func (fs *remoteFileSystem) Open(name string) (http.File, error) {
	log.Printf("origin: %s\n", name)
	path := fs.origin + name
	res, err := http.DefaultClient.Get(path)

	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	log.Println(path, res.ContentLength, res)

	if res.StatusCode != http.StatusOK || res.ContentLength <= 0 {
		return nil, http.ErrMissingFile
	}

	buf, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return nil, err
	}

	rd := bytes.NewReader(buf)
	var modtime time.Time

	if t, err := time.Parse(http.TimeFormat, res.Header.Get("Last-Modified")); err != nil {
		modtime = time.Now().UTC()
	} else {
		modtime = t
	}

	f := &file{
		ReadSeeker: rd,
		buf:        buf,
		fi: fileInfo{
			size:        rd.Len(),
			modtime:     modtime,
			basename:    path,
			contentType: res.Header.Get("Content-Type"),
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
	fs        http.FileSystem
	evictList *list.List
	cache     map[string]*list.Element
	mux       sync.RWMutex
	maxSize   int64
	size      int64
	maxItems  int
	items     int
}

func NewCache(fs http.FileSystem, maxItems int, maxSize int) http.FileSystem {
	return &memoryCacheFilesystem{
		maxItems:  maxItems,
		maxSize:   int64(maxSize),
		fs:        fs,
		cache:     make(map[string]*list.Element),
		evictList: list.New(),
	}
}

func (fs *memoryCacheFilesystem) get(name string) (http.File, bool) {
	fs.mux.Lock()
	defer fs.mux.Unlock()
	ent, ok := fs.cache[name]

	if !ok {
		return nil, false
	}

	fs.evictList.MoveToFront(ent)
	f := ent.Value.(*file)
	return f.readClone(), true
}

func (fs *memoryCacheFilesystem) add(name string, f *file) http.File {
	fs.mux.Lock()
	defer fs.mux.Unlock()

	// update if exists
	if v, ok := fs.cache[name]; ok {
		fs.evictList.MoveToFront(v)
		fs.size -= v.Value.(*file).fi.Size()
		v.Value = f
		fs.size += f.fi.Size()
		return f.readClone()
	}

	// add new
	fs.cache[name] = fs.evictList.PushFront(f)
	fs.size += f.fi.Size()

	if fs.evictList.Len() > fs.maxItems {
		fs.removeOldest()
	}

	return f.readClone()
}

// removeOldest removes the oldest item from the cache.
func (fs *memoryCacheFilesystem) removeOldest() {
	ent := fs.evictList.Back()

	if ent != nil {
		fs.removeElement(ent)
	}
}

// removeElement is used to remove a given list element from the cache
func (fs *memoryCacheFilesystem) removeElement(ent *list.Element) {
	fs.evictList.Remove(ent)
	f := ent.Value.(*file)
	fs.size -= f.fi.Size()
	delete(fs.cache, f.fi.Name())
}

func (fs *memoryCacheFilesystem) Open(name string) (http.File, error) {
	defer func() {
		//log.Printf("total memsize %.2f MB", float64(fs.size)/1.0e6)
	}()
	log.Printf("cache: %s\n", name)

	if f, ok := fs.get(name); ok {
		return f, nil
	}

	f, err := fs.fs.Open(name)

	if err != nil {
		return nil, err
	}

	rv := fs.add(name, f.(*file))
	return rv, nil
}

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

	_, haveType := w.Header()["Content-Type"]

	if !haveType {
		ff, ok := f.(*file)

		if ok && ff.fi.contentType != "" {
			w.Header().Set("Content-Type", ff.fi.contentType)
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
