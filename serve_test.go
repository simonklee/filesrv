package filesrv

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/simonz05/util/assert"
	"github.com/simonz05/util/httputil"
)

func TestServeConcurrent(t *testing.T) {
	ast := assert.NewAssertWithName(t, "TestServeConcurrent")
	fs := newFakeFs()
	cache := NewCache(fs, 2, 64)
	files := []string{"file1", "file2", "file3"}
	wg := sync.WaitGroup{}

	http.Handle("/", FileServer(cache))
	server := httptest.NewServer(nil)
	srvAddr := "http://" + server.Listener.Addr().String()
	defer func() {
		server.Close()
	}()

	for i := 0; i < 3; i++ {
		file := files[i]
		fs.mu.Lock()
		fs.files["/"+file] = newFile(file)
		fs.mu.Unlock()
		wg.Add(1)

		go func(file string) {
			for j := 0; j < 100; j++ {
				content, err := getFile(t, srvAddr+"/"+file)
				ast.Nil(err)

				if file != content {
					wg.Done()
					t.Fatalf("%s != %s\n", file, content)
				}
			}

			wg.Done()
		}(file)
	}

	wg.Wait()
}

func getFile(t *testing.T, uri string) (string, error) {
	req, err := httputil.NewRequest("GET", uri, nil, nil)

	if err != nil {
		t.Fatal(err)
	}

	res, err := req.Do()

	if err != nil {
		t.Fatal(err)
	}

	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)

	if err != nil {
		t.Fatal(err)
	}

	return string(body), nil
}
