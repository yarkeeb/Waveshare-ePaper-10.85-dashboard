# Attribution

This fork derives from the original Python dashboard and retains its used fonts and icons from
[czuryk/Waveshare-ePaper-10.85-dashboard](https://github.com/czuryk/Waveshare-ePaper-10.85-dashboard).
Existing upstream materials retain their original notices and licensing; this
rewrite does not relicense those materials.

The native Go display driver adapts the command sequences and partial-refresh
RAM handling from the original Waveshare `epd10in85.py` driver. Its permission
notice is retained in `third_party/waveshare-LICENSE` and in the original source in Git history.

The native Go Roborock V1 framing and cloud authentication implementation is
adapted from [Python-roborock/python-roborock](https://github.com/Python-roborock/python-roborock),
licensed under Apache-2.0. A copy is in `third_party/python-roborock-LICENSE`.
The Go implementation changes the language, transport integration, storage,
error handling, and exposed functionality; only dashboard status queries are
sent to vacuums.

Go dependency and runtime license notices are included under `third_party/go/`.
Module names and versions are recorded in `go.mod` and `go.sum`.
