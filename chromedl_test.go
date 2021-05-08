package chromedl

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/pkg/errors"
	"github.com/rusq/dlog"
)

func init() {
	dlog.SetDebug(true)
}

func fakeRunnerWithErr(err error) runnerFn {
	return func(ctx context.Context, actions ...chromedp.Action) error {
		return err
	}
}

func TestDownload(t *testing.T) {
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
			r, err := Download(context.Background(), tt.uri)
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

func TestMultiDL(t *testing.T) {

	var iterationC = make(chan int, 1)
	var format = "test data %d"
	var filefmt = "test%d.txt"
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		i := <-iterationC
		file := fmt.Sprintf(filefmt, i)
		data := fmt.Sprintf(format, i)
		serveFile(rw, r, file, []byte(data))
	}))
	defer srv.Close()

	t.Logf("test server at: %s", srv.URL)

	dlog.SetDebug(true)
	defer dlog.SetDebug(false)
	bi, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer bi.Stop()

	for i := 0; i < 100; i++ {
		t.Run(fmt.Sprintf("test %d", i), func(t *testing.T) {
			val := fmt.Sprintf(format, i)
			iterationC <- i

			r, err := bi.Download(context.Background(), srv.URL+"/"+fmt.Sprintf(filefmt, i))
			if err != nil {
				t.Fatalf("%+v", err)
			}
			if r == nil {
				t.Fatal("reader is nil")
			}
			got, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatalf("reader error: %s", err)
			}
			if !bytes.Equal(got, []byte(val)) {
				t.Errorf("data mismatch: got=%q vs want=%q", string(got), val)
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

func ExampleDownload() {
	const rbnzRates = "https://www.rbnz.govt.nz/-/media/ReserveBank/Files/Statistics/tables/b1/hb1-daily.xlsx?revision=5fa61401-a877-4607-b7ae-2e060c09935d"
	r, err := Download(context.Background(), rbnzRates)
	if err != nil {
		log.Fatal(err)
	}
	data, err := ioutil.ReadAll(r)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("file size > 0: %v\n", len(data) > 0)
	fmt.Printf("file signature: %s\n", string(data[0:2]))
	// Output:
	// file size > 0: true
	// file signature: PK
}

func TestInstance_readRequest(t *testing.T) {
	type fields struct {
		cfg           config
		ctx           context.Context
		allocCancel   context.CancelFunc
		browserCancel context.CancelFunc
		lnCancel      context.CancelFunc
		guidC         chan string
		requestIDC    chan network.RequestID
		requests      map[network.RequestID]bool
		tmpdir        string
	}
	type args struct {
		reqID network.RequestID
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		runner  runnerFn
		want    []byte
		wantErr bool
	}{
		{
			"no error",
			fields{},
			args{reqID: network.RequestID("123")},
			fakeRunnerWithErr(nil),
			nil,
			false,
		},
		{
			"error",
			fields{},
			args{reqID: network.RequestID("123")},
			fakeRunnerWithErr(errors.New("failed")),
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bi := &Instance{
				cfg:        tt.fields.cfg,
				ctx:        tt.fields.ctx,
				allocFn:    tt.fields.allocCancel,
				browserFn:  tt.fields.browserCancel,
				lnCancel:   tt.fields.lnCancel,
				guidC:      tt.fields.guidC,
				requestIDC: tt.fields.requestIDC,
				requests:   tt.fields.requests,
				tmpdir:     tt.fields.tmpdir,
			}
			oldRunner := runner
			defer func() {
				runner = oldRunner
			}()
			runner = tt.runner
			got, err := bi.readRequest(tt.args.reqID)
			if (err != nil) != tt.wantErr {
				t.Errorf("Instance.readRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Instance.readRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

// genFile generates a temporary file in directory dir with contents.
func genFile(t *testing.T, dir string, contents string) string {
	f, err := ioutil.TempFile(dir, "tmp*")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write([]byte(contents))
	return filepath.Base(f.Name())
}

func TestInstance_readFile(t *testing.T) {
	const contents = "test contents"
	testtmp, err := ioutil.TempDir("", "chromedl_test*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testtmp)

	type fields struct {
		cfg           config
		ctx           context.Context
		allocCancel   context.CancelFunc
		browserCancel context.CancelFunc
		lnCancel      context.CancelFunc
		guidC         chan string
		requestIDC    chan network.RequestID
		requests      map[network.RequestID]bool
		tmpdir        string
	}
	type args struct {
		name string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    []byte
		wantErr bool
	}{
		{
			"no error",
			fields{tmpdir: testtmp},
			args{name: genFile(t, testtmp, contents)},
			[]byte(contents),
			false,
		},
		{
			"non-existing",
			fields{tmpdir: testtmp},
			args{name: "$not here$"},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer os.Remove(tt.args.name)
			bi := &Instance{
				cfg:        tt.fields.cfg,
				ctx:        tt.fields.ctx,
				allocFn:    tt.fields.allocCancel,
				browserFn:  tt.fields.browserCancel,
				lnCancel:   tt.fields.lnCancel,
				guidC:      tt.fields.guidC,
				requestIDC: tt.fields.requestIDC,
				requests:   tt.fields.requests,
				tmpdir:     tt.fields.tmpdir,
			}
			got, err := bi.readFile(tt.args.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("Instance.readFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Instance.readFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInstance_waitTransfer(t *testing.T) {
	const contents = "test contents"
	testtmp, err := ioutil.TempDir("", "chromedl_test*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(testtmp)

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	type fields struct {
		cfg           config
		ctx           context.Context
		allocCancel   context.CancelFunc
		browserCancel context.CancelFunc
		lnCancel      context.CancelFunc
		guidC         chan string
		requestIDC    chan network.RequestID
		requests      map[network.RequestID]bool
		tmpdir        string
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		init    func(bi *Instance) error
		want    []byte
		wantErr bool
	}{
		{
			"file guid",
			fields{
				ctx:    context.Background(),
				guidC:  make(chan string, 1),
				tmpdir: testtmp,
			},
			args{ctx: context.Background()},
			func(bi *Instance) error {
				const filename = "test_data.txt"
				if err := ioutil.WriteFile(filepath.Join(bi.tmpdir, filename), []byte(contents), 0644); err != nil {
					return errors.WithStack(err)
				}
				bi.guidC <- filename
				return nil
			},
			[]byte(contents),
			false,
		},
		{
			"download cancelled",
			fields{
				ctx:    context.Background(),
				guidC:  make(chan string, 1),
				tmpdir: testtmp,
			},
			args{ctx: context.Background()},
			func(bi *Instance) error {
				bi.guidC <- ""
				return nil
			},
			nil,
			true,
		},
		{
			"cancelled main context",
			fields{
				guidC: make(chan string, 1),
			},
			args{ctx: context.Background()},
			func(bi *Instance) error {
				ctx, cancel := context.WithCancel(context.Background())
				bi.ctx = ctx
				cancel()

				// to ensure - sending the file on the guidC after some time.
				go time.AfterFunc(1*time.Second, func() {
					const filename = "test_data.txt"
					if err := ioutil.WriteFile(filepath.Join(bi.tmpdir, filename), []byte(contents), 0644); err != nil {
						t.Log(err)
					}
					bi.guidC <- filename
				})
				return nil
			},
			nil,
			true,
		},
		{
			"cancelled function context",
			fields{
				ctx:   context.Background(),
				guidC: make(chan string, 1),
			},
			args{ctx: cancelledCtx},
			func(bi *Instance) error {
				// to ensure - sending the file on the guidC after some time.
				go time.AfterFunc(1*time.Second, func() {
					const filename = "test_data.txt"
					if err := ioutil.WriteFile(filepath.Join(bi.tmpdir, filename), []byte(contents), 0644); err != nil {
						t.Log(err)
					}
					bi.guidC <- filename
				})
				return nil
			},
			nil,
			true,
		},
		{
			"request",
			fields{
				ctx:        context.Background(),
				requestIDC: make(chan network.RequestID, 1),
				tmpdir:     testtmp,
			},
			args{ctx: context.Background()},
			func(bi *Instance) error {
				bi.requestIDC <- network.RequestID("test")
				return nil
			},
			[]byte{},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldRunner := runner
			defer func() {
				runner = oldRunner
			}()
			runner = fakeRunnerWithErr(nil)
			bi := &Instance{
				cfg:        tt.fields.cfg,
				ctx:        tt.fields.ctx,
				allocFn:    tt.fields.allocCancel,
				browserFn:  tt.fields.browserCancel,
				lnCancel:   tt.fields.lnCancel,
				guidC:      tt.fields.guidC,
				requestIDC: tt.fields.requestIDC,
				requests:   tt.fields.requests,
				tmpdir:     tt.fields.tmpdir,
			}
			if err := tt.init(bi); err != nil {
				t.Fatalf("init failed: %s", err)
			}
			r, err := bi.waitTransfer(tt.args.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Instance.waitTransfer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if r == nil {
				return
			}
			got, err := ioutil.ReadAll(r)
			if err != nil {
				t.Fatalf("failed to read: %s", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Instance.waitTransfer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInstance_navigate(t *testing.T) {
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	type fields struct {
		cfg           config
		ctx           context.Context
		allocCancel   context.CancelFunc
		browserCancel context.CancelFunc
		lnCancel      context.CancelFunc
		guidC         chan string
		requestIDC    chan network.RequestID
		requests      map[network.RequestID]bool
		tmpdir        string
	}
	type args struct {
		ctx context.Context
		uri string
	}
	tests := []struct {
		name     string
		fields   fields
		runnerFn runnerFn
		args     args
		wantErr  bool
	}{
		{
			"no error",
			fields{
				ctx: context.Background(),
			},
			fakeRunnerWithErr(nil),
			args{ctx: context.Background(), uri: "test"},
			false,
		},
		{
			"error",
			fields{
				ctx: context.Background(),
			},
			fakeRunnerWithErr(errors.New("test error")),
			args{ctx: context.Background(), uri: "test"},
			true,
		},
		{
			"main context cancelled",
			fields{
				ctx: cancelledCtx,
			},
			fakeRunnerWithErr(nil),
			args{ctx: context.Background(), uri: "test"},
			true,
		},
		{
			"arg context cancelled",
			fields{
				ctx: context.Background(),
			},
			fakeRunnerWithErr(nil),
			args{ctx: cancelledCtx, uri: "test"},
			true,
		},
	}
	for _, tt := range tests {

		t.Run(tt.name, func(t *testing.T) {
			oldRunner := runner
			defer func() {
				runner = oldRunner
			}()
			runner = tt.runnerFn
			bi := &Instance{
				cfg:        tt.fields.cfg,
				ctx:        tt.fields.ctx,
				allocFn:    tt.fields.allocCancel,
				browserFn:  tt.fields.browserCancel,
				lnCancel:   tt.fields.lnCancel,
				guidC:      tt.fields.guidC,
				requestIDC: tt.fields.requestIDC,
				requests:   tt.fields.requests,
				tmpdir:     tt.fields.tmpdir,
			}
			if err := bi.navigate(tt.args.ctx, tt.args.uri); (err != nil) != tt.wantErr {
				t.Errorf("Instance.navigate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInstance_eventHandler(t *testing.T) {
	t.Run("download progress event", func(t *testing.T) {
		bi := &Instance{
			guidC: make(chan string, 1),
		}
		bi.eventHandler(&page.EventDownloadProgress{
			State: page.DownloadProgressStateCompleted,
			GUID:  "testGUID",
		})
		select {
		case guid := <-bi.guidC:
			if !strings.EqualFold(guid, "testGUID") {
				t.Errorf("wrong value: want=%q, got=%q", "testGUID", guid)
			}
		default:
			t.Error("did not receive anything")
		}
	})
	t.Run("download cancelled event", func(t *testing.T) {
		bi := &Instance{
			guidC: make(chan string, 1),
		}
		bi.eventHandler(&page.EventDownloadProgress{
			State: page.DownloadProgressStateCanceled,
			GUID:  "testGUID",
		})
		select {
		case guid := <-bi.guidC:
			if guid != "" {
				t.Errorf("wrong value: want=%q, got=%q", "", guid)
			}
		default:
			t.Error("did not receive anything")
		}
	})
	t.Run("event request will be sent", func(t *testing.T) {
		bi := &Instance{
			requestIDC: make(chan network.RequestID, 1),
			requests:   make(map[network.RequestID]bool),
		}

		// emulate download start
		bi.eventHandler(&network.EventRequestWillBeSent{
			RequestID: "ID",
			Request:   &network.Request{URL: "http://example"},
		})
		if !bi.requests["ID"] {
			t.Error("request has not been registered")
		}

		// emulate download complete
		bi.eventHandler(&network.EventLoadingFinished{RequestID: "ID"})
		select {
		case reqID := <-bi.requestIDC:
			if reqID != "ID" {
				t.Errorf("request name mismatch: want %q, got %q", "ID", reqID)
			}
		default:
			t.Error("no request ID")
		}

		// check if request has been removed.
		if bi.requests["ID"] {
			t.Error("request has not been removed")
		}
	})
}
