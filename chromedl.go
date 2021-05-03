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

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/pkg/errors"
	"github.com/rusq/dlog"
)

// tempPrefix is the prefix for the temp directory.
const tempPrefix = "chromedl"

// UserAgent is the user agent string sent to the server.  Can be changed by the
// caller.
var UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.93 Safari/537.36"

// Get downloads a file from the provided uri using the chromedp capabilities.
// It will return the reader with the file contents (buffered), and an error if
// any.  If the error is present, reader may not be nil if the file was
// downloaded and read successfully.  It will store the file in the temporary
// directory once the download is complete, then buffer it and try to cleanup
// afterwards.  Set the timeout on context if required, by default no timeout is
// set.
func Get(ctx context.Context, uri string) (io.Reader, error) {

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent(UserAgent),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts[:]...)
	defer cancel()

	// also set up a custom logger
	ctx, cancel = chromedp.NewContext(allocCtx, chromedp.WithLogf(dlog.Printf), chromedp.WithDebugf(dlog.Debugf))
	defer cancel()

	// set up channels for different type of download requests.
	guidC := make(chan string)
	defer close(guidC)
	reqIDC := make(chan network.RequestID)
	defer close(reqIDC)

	var requestId = map[network.RequestID]bool{}

	chromedp.ListenTarget(ctx, func(v interface{}) {
		switch ev := v.(type) {
		case *page.EventDownloadProgress:
			dlog.Debugf(">>> current download state: %s", ev.State.String())
			if ev.State == page.DownloadProgressStateCompleted {
				guidC <- ev.GUID
			}
		case *network.EventRequestWillBeSent:
			dlog.Debugf(">>> EventRequestWillBeSent: %v: %v", ev.RequestID, ev.Request.URL)
			if ev.Request.URL == uri {
				requestId[ev.RequestID] = true
			}
		case *network.EventLoadingFinished:
			dlog.Debugf(">>> EventLoadingFinished: %v", ev.RequestID)
			if requestId[ev.RequestID] {
				reqIDC <- ev.RequestID
			}
		default:
			dlog.Debugf("*** EVENT: %[1]T\n", v)
		}
	})

	tmpdir, err := ioutil.TempDir("", tempPrefix+"*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			scriptID, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
			if err != nil {
				return err
			}
			dlog.Debugf("scriptID: %s", scriptID)
			return nil
		}),
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).WithDownloadPath(tmpdir),
		chromedp.Navigate(uri),
	); err != nil && !strings.Contains(err.Error(), "net::ERR_ABORTED") {
		// Note: Ignoring the net::ERR_ABORTED page error is essential here since downloads
		// will cause this error to be emitted, although the download will still succeed.
		return nil, errors.WithStack(err)
	}

	return waitComplete(ctx, guidC, reqIDC, tmpdir)
}

// waitComplete waits to receive the completed download from either guid channel
// or request ID channel.  Then it does what it takes to open the received data,
// buffer it and return the reader.
func waitComplete(ctx context.Context, guidC <-chan string, reqIDC <-chan network.RequestID, tmpdir string) (io.Reader, error) {
	// Listening to both available channes to return the download.

	var b []byte
	var err error
	select {
	case <-ctx.Done():
		return nil, errors.WithStack(ctx.Err())
	case fileGUID := <-guidC:
		b, err = readFile(tmpdir, fileGUID)
	case reqID := <-reqIDC:
		b, err = readRequest(ctx, reqID)
	}
	return bytes.NewReader(b), err
}

func readFile(tmpdir string, name string) ([]byte, error) {
	// We can predict the exact file location and name here because of how we configured
	// SetDownloadBehavior and WithDownloadPath
	downloadPath := filepath.Join(tmpdir, name)
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

func readRequest(ctx context.Context, reqID network.RequestID) ([]byte, error) {
	var b []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		b, err = network.GetResponseBody(reqID).Do(ctx)
		return errors.WithStack(err)
	})); err != nil {
		return nil, errors.WithStack(err)
	}
	return b, nil
}
