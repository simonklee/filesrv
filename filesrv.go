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
	"sync"
	"time"

	"github.com/simonz05/util/log"
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
	path := fs.origin + name
	res, err := http.DefaultClient.Get(path)

	if err != nil {
		return nil, err
	}

	log.Println(path, res)

	if res.StatusCode != http.StatusOK || res.ContentLength <= 0 {
		return nil, http.ErrMissingFile
	}

	buf, err := ioutil.ReadAll(res.Body)

	if err != nil {
		return nil, err
	}

	rd := bytes.NewReader(buf)

	f := &file{
		ReadSeeker: rd,
		buf:        buf,
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
	fs        http.FileSystem
	size      int
	evictList *list.List
	cache     map[string]*list.Element
	mux       sync.RWMutex
}

func NewCache(fs http.FileSystem, size int, bytes int) http.FileSystem {
	return &memoryCacheFilesystem{
		size:      size,
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

func (fs *memoryCacheFilesystem) add(name string, f http.File) http.File {
	fs.mux.Lock()
	defer fs.mux.Unlock()

	// Check for existing item
	if v, ok := fs.cache[name]; ok {
		fs.evictList.MoveToFront(v)
		v.Value = f
		return f.(*file).readClone()
	}

	// Add new item
	fs.cache[name] = fs.evictList.PushFront(f)

	evict := fs.evictList.Len() > fs.size

	// Verify size not exceeded
	if evict {
		fs.removeOldest()
	}

	return f.(*file).readClone()
}

// removeOldest removes the oldest item from the cache.
func (fs *memoryCacheFilesystem) removeOldest() {
	ent := fs.evictList.Back()

	if ent != nil {
		fs.removeElement(ent)
	}
}

// removeElement is used to remove a given list element from the cache
func (fs *memoryCacheFilesystem) removeElement(e *list.Element) {
	fs.evictList.Remove(e)
	f := e.Value.(*file)
	delete(fs.cache, f.fi.Name())
}

func (fs *memoryCacheFilesystem) Open(name string) (http.File, error) {
	if f, ok := fs.get(name); ok {
		return f, nil
	}

	f, err := fs.fs.Open(name)

	if err != nil {
		return nil, err
	}

	return fs.add(name, f), nil
}
