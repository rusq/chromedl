========================
 Chrome File Downloader
========================

The sole purpose of this package is to download files with headless
Chrome.

Package ChromeDL uses chromedp to download the files.  It may be of
use when one needs to download a file from a protected website that
doesn't allow regular methods, such as curl or http.Get().

It is heavily based on this `chromedp example`_.

Thanks to `@ZekeLu`_ for help in getting this going.

Compatibility
-------------

Tested with:
* Chrome (stable) v90.0.4430.93.
* github.com/chromedp/chromedp v0.6.12
* github.com/chromedp/cdproto v0.0.0-20210323015217-0942afbea50e

Newer versions require code change, as described in `this issue`_, as
it uses deprecated in newer protocol version calls to be compatible
with current stable version of Chrome (see above).


.. _`this issue`: https://github.com/chromedp/chromedp/issues/807
.. _`chromedp example`: https://github.com/chromedp/examples/tree/master/download_file
.. _`@ZekeLu`: https://github.com/ZekeLu
