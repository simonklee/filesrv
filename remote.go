package filesrv

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/simonz05/util/log"
)

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
