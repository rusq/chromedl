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
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/pkg/errors"
	"github.com/rusq/dlog"
)

// tempPrefix is the prefix for the temp directory.
const tempPrefix = "chromedl"

// Get downloads a file from provided uri using the chromedp capabilities.  It
// will return the reader with the file contents (buffered), and an error if
// any.  If the error is present, reader may not be nil if the file was
// downloaded and read successfully.  It will store the file in the temporary
// directory once the download is complete, then buffer it and try to cleanup
// afterwards.  Set the timeout on context if required, by default no timeout is
// set.
func Get(ctx context.Context, uri string) (io.Reader, error) {
	// create chrome instance
	ctx, cancel := chromedp.NewContext(
		ctx,
		chromedp.WithLogf(dlog.Printf),
		chromedp.WithDebugf(dlog.Debugf),
	)
	defer cancel()

	// create a timeout as a safety net to prevent any infinite wait loops
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// set up a channel so we can block later while we monitor the download progress
	guidC := make(chan string)
	defer close(guidC)
	// set up a listener to watch the download events and close the channel when complete
	// this could be expanded to handle multiple downloads through creating a guid map,
	// monitor download urls via EventDownloadWillBegin, etc
	chromedp.ListenTarget(ctx, func(v interface{}) {
		switch ev := v.(type) {
		case *page.EventDownloadProgress:
			dlog.Debugf("current download state: %s\n", ev.State.String())
			if ev.State == page.DownloadProgressStateCompleted {
				guidC <- ev.GUID
			}
		default:
			dlog.Debugf("*** EVENT: %[1]T\n", v)
		}
	})

	tmpdir, err := ioutil.TempDir("", tempPrefix+"*")
	if err != nil {
		return nil, err
	}
	if err := chromedp.Run(ctx,
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).WithDownloadPath(tmpdir),
		chromedp.Navigate(uri),
	); err != nil && !strings.Contains(err.Error(), "net::ERR_ABORTED") {
		// Note: Ignoring the net::ERR_ABORTED page error is essential here since downloads
		// will cause this error to be emitted, although the download will still succeed.
		return nil, errors.WithStack(err)
	}

	// This will block until the chromedp listener closes the channel
	fileGUID := <-guidC

	// We can predict the exact file location and name here because of how we configured
	// SetDownloadBehavior and WithDownloadPath
	downloadPath := filepath.Join(tmpdir, fileGUID)
	b, err := ioutil.ReadFile(downloadPath)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	log.Printf("Download Complete: %s", downloadPath)
	if err := os.Remove(downloadPath); err != nil {
		return bytes.NewReader(b), err
	}
	return bytes.NewReader(b), os.RemoveAll(tmpdir)
}
