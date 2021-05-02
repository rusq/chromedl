package chromedl

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/rusq/dlog"
)

func init() {
	dlog.SetDebug(true)
}

func TestBrowserDL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		serveFile(rw, r, "test.txt", []byte("test data"))
	}))
	defer srv.Close()
	t.Logf("test server at: %s", srv.URL)

	tests := []struct {
		name    string
		uri     string
		want    []byte
		wantErr bool
	}{
		{"x", srv.URL + "/test.txt", []byte("test data"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := Get(context.Background(), tt.uri)
			if (err != nil) != tt.wantErr {
				t.Errorf("Get() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if r == nil {
				return
			}
			got, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatalf("reader error: %s", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Get() = %v, want %v", got, tt.want)
			}
		})
	}
}

func serveFile(w http.ResponseWriter, r *http.Request, filename string, data []byte) {
	w.Header().Set("Content-Disposition", "attachment; filename="+filename+"")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Content-Control", "private, no-transform, no-store, must-revalidate")

	http.ServeContent(w, r, filename, time.Now(), bytes.NewReader(data))
}