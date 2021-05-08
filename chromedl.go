// Package ChromeDL uses chromedp to download the files.  It may come handy when
// one needs to get a file from a protected website that doesn't allow regular
// methods, such as curl or http.Get().
//
// It is heavily based on https://github.com/chromedp/examples/tree/master/download_file
// with minor modifications.
package chromedl

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/pkg/errors"
	"github.com/rusq/dlog"
)

// tempPrefix is the prefix for the temp directory.
const tempPrefix = "chromedl"

// DefaultUA is the default user agent string that will be used by the browser instance.  Can be changed
const DefaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.93 Safari/537.36"

// Instance is the browser instance that will be used for downloading files.
type Instance struct {
	cfg config

	ctx context.Context // context with the browser

	allocFn   context.CancelFunc // allocator cancel func
	browserFn context.CancelFunc // browser cancel func
	lnCancel  context.CancelFunc // listener cancel func

	guidC      chan string
	requestIDC chan network.RequestID

	mu       sync.Mutex
	requests map[network.RequestID]bool

	tmpdir string
}

type config struct {
	UserAgent string
}

type runnerFn = func(ctx context.Context, actions ...chromedp.Action) error

// to be able to mock in tests.
var runner runnerFn = chromedp.Run

type Option func(*config)

// OptUserAgent allows setting the user agent for the browser.
func OptUserAgent(ua string) Option {
	return func(c *config) {
		if ua == "" {
			c.UserAgent = DefaultUA
		}
		c.UserAgent = ua
	}
}

// New creates a new Instance, starting up the headless chrome to do the download.
// Once finished, call Stop to terminate the browser.
func New(options ...Option) (*Instance, error) {

	cfg := config{
		UserAgent: DefaultUA,
	}
	for _, opt := range options {
		opt(&cfg)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(cfg.UserAgent),
	)

	allocCtx, aCancel := chromedp.NewExecAllocator(context.Background(), opts[:]...)
	ctx, cCancel := chromedp.NewContext(allocCtx, chromedp.WithLogf(dlog.Printf), chromedp.WithDebugf(dlog.Debugf))

	return newInstance(ctx, cfg, aCancel, cCancel)
}

func newInstance(ctx context.Context, cfg config, allocCFn, ctxCFn context.CancelFunc) (*Instance, error) {
	tmpdir, err := ioutil.TempDir("", tempPrefix+"*")
	if err != nil {
		return nil, err
	}

	bi := Instance{
		cfg: cfg,

		ctx:       ctx,
		allocFn:   allocCFn,
		browserFn: ctxCFn,

		guidC:      make(chan string),
		requestIDC: make(chan network.RequestID),

		requests: map[network.RequestID]bool{},

		tmpdir: tmpdir,
	}

	bi.startListener()

	return &bi, nil
}

// ErrNoChrome indicates that there's no chrome instance in the context.
var ErrNoChrome = errors.New("no chrome instance in the context")

// NewWithChromeCtx creates new Instance for existing browser instance.  Stop will not terminate
// the browser, but will cancel the event listener.
func NewWithChromeCtx(taskCtx context.Context, options ...Option) (*Instance, error) {
	if chrome := chromedp.FromContext(taskCtx); chrome == nil {
		return nil, ErrNoChrome
	}
	return newInstance(taskCtx, config{}, nil, nil)
}

func (bi *Instance) Stop() error {
	bi.stopListener()
	// close download channels
	close(bi.guidC)
	close(bi.requestIDC)

	// cancel contexts if cancel functions are set
	if bi.allocFn != nil {
		bi.browserFn()
	}
	if bi.allocFn != nil {
		bi.allocFn()
	}

	// remove temporary dir with any residual files
	return os.RemoveAll(bi.tmpdir)
}

// Get downloads a file from the provided uri using the chromedp capabilities.
// It will return the reader with the file contents (buffered), and an error if
// any.  If the error is present, reader may not be nil if the file was
// downloaded and read successfully.  It will store the file in the temporary
// directory once the download is complete, then buffer it and try to cleanup
// afterwards.  Set the timeout on context if required, by default no timeout is
// set.  Optionally one can pass the configuration options for the downloader.
func Get(ctx context.Context, uri string, opts ...Option) (io.Reader, error) {
	bi, err := New(opts...)
	if err != nil {
		return nil, err
	}
	defer bi.Stop()
	return bi.Get(ctx, uri)
}

// stopListener stops the Listener.
func (bi *Instance) stopListener() {
	if bi.lnCancel == nil {
		return
	}
	// cancel listener context
	bi.mu.Lock()
	defer bi.mu.Unlock()

	bi.lnCancel()
	bi.lnCancel = nil
}

func (bi *Instance) startListener() {
	bi.mu.Lock()
	defer bi.mu.Unlock()

	lnctx, cancel := context.WithCancel(bi.ctx)
	bi.lnCancel = cancel

	chromedp.ListenTarget(lnctx, bi.eventHandler)
}

// eventHandler handles the download event.
func (bi *Instance) eventHandler(v interface{}) {
	switch ev := v.(type) {
	case *page.EventDownloadProgress:
		dlog.Debugf(">>> current download state: %s", ev.State.String())
		if ev.State == page.DownloadProgressStateCompleted {
			bi.guidC <- ev.GUID
		} else if ev.State == page.DownloadProgressStateCanceled {
			bi.guidC <- ""
		}
	case *network.EventRequestWillBeSent:
		dlog.Debugf(">>> EventRequestWillBeSent: %v: %v", ev.RequestID, ev.Request.URL)

		bi.mu.Lock()
		bi.requests[ev.RequestID] = true
		bi.mu.Unlock()

	case *network.EventLoadingFinished:
		dlog.Debugf(">>> EventLoadingFinished: %v", ev.RequestID)
		if bi.requests[ev.RequestID] {
			bi.requestIDC <- ev.RequestID

			bi.mu.Lock()
			delete(bi.requests, ev.RequestID)
			bi.mu.Unlock()
		}
	// TODO handle nework.EventLoadingFailed
	default:
		dlog.Debugf("*** EVENT: %[1]T\n", v)
	}
}

// Get gets the file.
func (bi *Instance) Get(ctx context.Context, uri string) (io.Reader, error) {
	if err := bi.navigate(ctx, uri); err != nil {
		return nil, err
	}
	return bi.waitTransfer(ctx)
}

func (bi *Instance) navigate(ctx context.Context, uri string) error {
	var errC = make(chan error, 1)

	go func() {
		errC <- runner(bi.ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				scriptID, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
				if err != nil {
					return err
				}
				dlog.Debugf("scriptID: %s", scriptID)
				return nil
			}),
			browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).WithDownloadPath(bi.tmpdir),
			chromedp.Navigate(uri),
		)
	}()

	select {
	case err := <-errC:
		if err != nil && !strings.Contains(err.Error(), "net::ERR_ABORTED") {
			// Note: Ignoring the net::ERR_ABORTED page error is essential here since downloads
			// will cause this error to be emitted, although the download will still succeed.
			return errors.WithStack(err)
		}
	case <-bi.ctx.Done():
		return errors.WithStack(bi.ctx.Err())
	case <-ctx.Done():
		return errors.WithStack(ctx.Err())
	}

	return nil
}

// waitTransfer waits to receive the completed download from either guid channel
// or request ID channel.  Then it does what it takes to open the received data,
// buffer it and return the reader.
func (bi *Instance) waitTransfer(ctx context.Context) (io.Reader, error) {
	// Listening to both available channes to return the download.
	var (
		b   []byte
		err error
	)
	select {
	case <-ctx.Done():
		return nil, errors.WithStack(ctx.Err())
	case <-bi.ctx.Done():
		return nil, errors.WithStack(bi.ctx.Err())
	case filename := <-bi.guidC:
		if filename == "" {
			return nil, errors.New("download was cancelled")
		}
		b, err = bi.readFile(filename)
	case reqID := <-bi.requestIDC:
		b, err = bi.readRequest(reqID)
	}
	return bytes.NewReader(b), err
}

func (bi *Instance) readFile(name string) ([]byte, error) {
	// We can predict the exact file location and name here because of how we configured
	// SetDownloadBehavior and WithDownloadPath
	downloadPath := filepath.Join(bi.tmpdir, name)
	b, err := ioutil.ReadFile(downloadPath)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	dlog.Debugf("Download Complete: %s", downloadPath)
	if err := os.Remove(downloadPath); err != nil {
		return b, err
	}
	return b, nil
}

func (bi *Instance) readRequest(reqID network.RequestID) ([]byte, error) {
	var b []byte
	if err := runner(bi.ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		b, err = network.GetResponseBody(reqID).Do(ctx)
		return errors.WithStack(err)
	})); err != nil {
		return nil, errors.WithStack(err)
	}
	return b, nil
}
