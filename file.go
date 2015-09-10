package filesrv

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"time"
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
