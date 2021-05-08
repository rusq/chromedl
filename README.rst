========================
 Chrome File Downloader
========================

.. image:: https://pkg.go.dev/badge/github.com/rusq/chromedl.svg
   :alt: Go Reference
   :target: https://pkg.go.dev/github.com/rusq/chromedl 


.. contents::
   :depth: 2

The sole purpose of this package is to download files from the Internets with
headless Chrome bypassing the Cloudflare and maybe some other annoying browser
checks.

It does so by implementing the solutions posted in "`bypass headless chrome
detection issue`_" for chromedp_.

This library may help you if the other download methods don't work, i.e. curl or
the standard `http.Get()`.

The implementation is based on this `chromedp example`_.

Thanks to `@ZekeLu`_ for huge help in getting this going.

Compatibility
-------------

Tested with:

* Chrome (stable) v90.0.4430.93.
* github.com/chromedp/chromedp v0.6.12
* github.com/chromedp/cdproto v0.0.0-20210323015217-0942afbea50e

Newer versions of Chrome will require some code changes, as described in `this
issue`_, as it uses calls that are deprecated in newer protocol version in order
to be compatible with current stable version of Chrome (see above).

When using headless-shell docker image, please use the following tag::

  FROM chromedp/headless-shell:90.0.4430.93


LICENCES
--------
chromedp_: Copyright (c) 2016-2020 Kenneth Shaw


.. _`this issue`: https://github.com/chromedp/chromedp/issues/807
.. _`chromedp example`: https://github.com/chromedp/examples/tree/master/download_file
.. _`@ZekeLu`: https://github.com/ZekeLu
.. _chromedp: https://github.com/chromedp/chromedp
.. _`bypass headless chrome detection issue`: https://github.com/chromedp/chromedp/issues/396
