package filesrv

import (
	"bytes"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/simonz05/util/assert"
)

func newFile(content string) *file {
	buf := []byte(content)
	rd := bytes.NewReader(buf)
	modtime := time.Date(2015, 9, 1, 15, 3, 1, 0, time.UTC)

	return &file{
		ReadSeeker: rd,
		buf:        buf,
		fi: fileInfo{
			size:        rd.Len(),
			modtime:     modtime,
			basename:    content,
			contentType: "application/text",
			etag:        "tag",
		},
	}
}

type fakeFs struct {
	files     map[string]*file
	filesStat map[string]int
	openCnt   int
	mu        sync.Mutex
}

func newFakeFs() *fakeFs {
	return &fakeFs{
		files:     make(map[string]*file),
		filesStat: make(map[string]int),
	}
}

func (fs *fakeFs) Open(name string) (http.File, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.openCnt++
	f, ok := fs.files[name]

	if !ok {
		return nil, errors.New("not exist")
	}

	fs.filesStat[name]++
	return http.File(f), nil
}

func TestCache(t *testing.T) {
	ast := assert.NewAssertWithName(t, "TestCache")
	fs := newFakeFs()
	cache := NewCache(fs, 2, 64)
	file1, file2, file3 := "file1", "file2", "file3"

	_, err := cache.Open(file1)
	ast.NotNil(err)
	ast.Equal(1, fs.openCnt)

	// add file to fs
	fs.files[file1] = newFile(file1)

	// open file1
	f, err := cache.Open(file1)
	ast.Nil(err)
	fi, _ := f.Stat()
	ast.Equal(file1, fi.Name())
	ast.Equal(1, fs.filesStat[file1])
	ast.Equal(2, fs.openCnt)

	// open file1 (again)
	f, err = cache.Open(file1)
	ast.Nil(err)
	fi, _ = f.Stat()
	ast.Equal(file1, fi.Name())
	ast.Equal(1, fs.filesStat[file1])
	ast.Equal(2, fs.openCnt)

	// add files to fs
	fs.files[file2] = newFile(file2)
	fs.files[file3] = newFile(file3)

	// open file2
	f, err = cache.Open(file2)
	ast.Nil(err)
	fi, _ = f.Stat()
	ast.Equal(file2, fi.Name())
	ast.Equal(1, fs.filesStat[file2])
	ast.Equal(3, fs.openCnt)

	// open file3 (file1 should be pushed out)
	f, err = cache.Open(file3)
	ast.Nil(err)
	fi, _ = f.Stat()
	ast.Equal(file3, fi.Name())
	ast.Equal(1, fs.filesStat[file3])
	ast.Equal(4, fs.openCnt)

	// open file1 (again, should have been pushed out)
	f, err = cache.Open(file1)
	ast.Nil(err)
	fi, _ = f.Stat()
	ast.Equal(file1, fi.Name())
	ast.Equal(2, fs.filesStat[file1])
	ast.Equal(5, fs.openCnt)
}

func TestCacheConcurrent(t *testing.T) {
	ast := assert.NewAssertWithName(t, "TestCacheConcurrent")
	fs := newFakeFs()
	cache := NewCache(fs, 2, 64)
	files := []string{"file1", "file2", "file3"}
	wg := sync.WaitGroup{}

	for i := 0; i < 3; i++ {
		file := files[i]
		fs.mu.Lock()
		fs.files[file] = newFile(file)
		fs.mu.Unlock()
		wg.Add(1)

		go func(file string) {
			for j := 0; j < 100; j++ {
				_, err := cache.Open(file)
				ast.Nil(err)
			}

			wg.Done()
		}(file)
	}

	wg.Wait()
}
