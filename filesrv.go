// Copyright 2015 Simon Zimmermann. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package filesrv

import (
	"bytes"
	"container/list"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
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
	etag        string
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

func getContentType(r *http.Response, rd io.ReadSeeker, name string) (string, error) {
	const sniffLen = 512
	ctypes, haveType := r.Header["Content-Type"]
	var ctype string
	if !haveType {
		ctype = mime.TypeByExtension(filepath.Ext(name))
		if ctype == "" {
			// read a chunk to decide between utf-8 text and binary
			var buf [sniffLen]byte
			n, _ := io.ReadFull(rd, buf[:])
			ctype = http.DetectContentType(buf[:n])
			_, err := rd.Seek(0, os.SEEK_SET) // rewind to output whole file

			if err != nil {
				return "", err
			}
		}
	} else if len(ctypes) > 0 {
		ctype = ctypes[0]
	}

	return ctype, nil
}

func getModtime(r *http.Response) (modtime time.Time) {
	if t, err := time.Parse(http.TimeFormat, r.Header.Get("Last-Modified")); err != nil {
		modtime = time.Now().UTC()
	} else {
		modtime = t
	}
	return
}

func getETag(r *http.Response, rd io.ReadSeeker) (etag string) {
	etag = r.Header.Get("Etag")
	etag = strings.Trim(etag, "\"")

	if etag == "" {
		hash := md5.New()
		io.Copy(hash, rd)
		etag = hex.EncodeToString(hash.Sum(nil))
	}

	return
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

	contentType, err := getContentType(res, rd, name)

	if err != nil {
		return nil, err
	}

	etag := getETag(res, rd)
	modtime := getModtime(res)

	f := &file{
		ReadSeeker: rd,
		buf:        buf,
		fi: fileInfo{
			size:        rd.Len(),
			modtime:     modtime,
			basename:    path,
			contentType: contentType,
			etag:        etag,
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
	fs          http.FileSystem
	evictList   *list.List
	cache       map[string]*list.Element
	mux         sync.RWMutex
	maxSize     int64
	size        int64
	maxItems    int
	items       int
	invalidator *cacheInvalidator
}

func NewCache(fs http.FileSystem, maxItems int, maxSize int) http.FileSystem {
	mc := &memoryCacheFilesystem{
		maxItems:  maxItems,
		maxSize:   int64(maxSize),
		fs:        fs,
		cache:     make(map[string]*list.Element),
		evictList: list.New(),
	}
	mc.invalidator = newCacheInvalidator(func(name string) {
		mc.del(name)
	})
	return mc
}

type centry struct {
	file *file
	name string
}

func (fs *memoryCacheFilesystem) get(name string) (http.File, bool) {
	fs.mux.Lock()
	defer fs.mux.Unlock()
	ent, ok := fs.cache[name]

	if !ok {
		return nil, false
	}

	fs.evictList.MoveToFront(ent)
	f := ent.Value.(*centry).file
	return f.readClone(), true
}

func (fs *memoryCacheFilesystem) add(name string, f *file) http.File {
	fs.mux.Lock()
	defer fs.mux.Unlock()

	// delete existing item
	if v, ok := fs.cache[name]; ok {
		fs.removeElement(v)
	}

	// add new
	ent := &centry{file: f, name: name}
	fs.cache[name] = fs.evictList.PushFront(ent)
	fs.size += f.fi.Size()

	if fs.evictList.Len() > fs.maxItems {
		fs.removeOldest()
	}

	fs.invalidator.Add(ent)
	return f.readClone()
}

func (fs *memoryCacheFilesystem) del(name string) bool {
	fs.mux.Lock()
	defer fs.mux.Unlock()
	ent, ok := fs.cache[name]

	if ok {
		fs.removeElement(ent)
	}

	return ok
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
	cent := ent.Value.(*centry)
	fs.size -= cent.file.fi.Size()
	delete(fs.cache, cent.name)
	fs.invalidator.Del(cent)
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

type cacheInvalidator struct {
	wg      sync.WaitGroup
	Period  time.Duration
	quit    chan bool
	delfn   func(name string)
	items   map[string]fileInfo
	added   map[string]bool
	removed map[string]bool
	lastmod int // relative clock
	mux     sync.Mutex
}

func newCacheInvalidator(delfn func(name string)) *cacheInvalidator {
	ci := &cacheInvalidator{
		quit:    make(chan bool),
		items:   make(map[string]fileInfo),
		added:   make(map[string]bool),
		removed: make(map[string]bool),
		lastmod: 0,
		Period:  time.Second * 30,
		delfn:   delfn,
	}

	ci.wg.Add(1)
	go func() {
		ci.run()
		ci.wg.Done()
	}()

	return ci
}

func (ci *cacheInvalidator) Add(ent *centry) {
	ci.mux.Lock()
	defer ci.mux.Unlock()
	ci.items[ent.name] = ent.file.fi
	ci.added[ent.name] = true
	ci.lastmod++
}

func (ci *cacheInvalidator) Del(ent *centry) {
	ci.mux.Lock()
	defer ci.mux.Unlock()
	delete(ci.items, ent.name)
	ci.removed[ent.name] = true
	ci.lastmod++
}

func (ci *cacheInvalidator) run() {
	items := make(map[string]fileInfo)
	lastmod := 0

	for {
		select {
		case <-ci.quit:
			log.Println("invalidator exp: quit")
			return
		case <-time.After(ci.Period):
			start := time.Now()
			ci.mux.Lock()
			// check if we need to update local state
			if ci.lastmod != lastmod {
				// update local and ci state
				lastmod = ci.lastmod
				for k := range ci.removed {
					delete(items, k)
					delete(ci.removed, k)
				}
				for k := range ci.added {
					items[k] = ci.items[k]
					delete(ci.added, k)
				}
			}
			ci.mux.Unlock()

			invalidCnt := 0

			// items is now up to date with ci.items
			for name, fi := range items {
				uptodate, err := ci.check(fi)

				if err != nil {
					// todo
					log.Print(err)
					continue
				}

				if !uptodate {
					log.Printf("invalidate: %s", name)
					ci.delfn(name)
					invalidCnt++
				}
			}

			if invalidCnt > 0 {
				dt := time.Now().Sub(start)
				log.Printf("invalidator: timer %s", dt)
			}
		}
	}
}

func (ci *cacheInvalidator) check(fi fileInfo) (bool, error) {
	req, err := http.NewRequest("HEAD", fi.Name(), nil)

	if err != nil {
		return false, err
	}

	req.Header.Add("If-Modified-Since", fi.modtime.UTC().Format(http.TimeFormat))
	req.Header.Add("If-None-Match", fi.etag)

	res, err := http.DefaultClient.Do(req)

	if err != nil {
		return false, err
	}

	defer res.Body.Close()
	log.Println(fi.Name(), res.StatusCode, res.Status)

	switch res.StatusCode {
	case http.StatusNotModified:
		return true, nil
	case 429, http.StatusRequestTimeout:
		return false, errors.New("retry")
	default:
		return false, nil
	}
}

func (ci *cacheInvalidator) Close() error {
	log.Println("invalidator: Closing ...")
	close(ci.quit)
	done := make(chan bool)

	go func(done chan bool) {
		ci.wg.Wait()
		done <- true
	}(done)

	select {
	case <-done:
		break
	case <-time.After(5 * time.Second):
		return fmt.Errorf("invalidator: Timed out")
	}
	log.Println("invalidator: Closed")
	return nil
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
