# Go vs Python benchmark — 15 October 2026

This is the pre-optimization baseline. See the [grayscale packing optimization results](OPTIMIZATION.md) for the subsequent comparison.

On this Apple M5 Pro Mac, Go renders faster, starts the renderer harness faster, and uses less peak memory. Python/Pillow is faster for the complete render-and-pack path. The Go framebuffer packing step accounts for the difference.

## Setup

- Apple M5 Pro, 64 GiB RAM, macOS 26.6.2, arm64.
- Go 1.26.7; Python 3.12.14; Pillow 12.3.0.
- Go production code: `ebb0ee9fb5c620066cb8207d39872e9482b573ef`.
- Original Python: `ceced5eb8a0c17c58d73064a687f924bc3e3b461`, unchanged extracted renderer and packing function.
- Shared fixed data and time; 1360×480 output; five layouts. Ten warm-up frames, five independent batches of 100 iterations per operation/layout/implementation, and ten fresh-process startup samples.
- Values below are medians; parentheses show the minimum and maximum batch means. Lower is better. These ranges are not per-frame percentiles.

## Default layout

| Measurement | Go | Python/Pillow |
| --- | ---: | ---: |
| Rendering (ms/frame) | 3.285 (3.253–3.300) | 3.754 (3.729–3.808) |
| Framebuffer packing (ms/frame) | 1.743 (1.733–1.747) | 0.125 (0.122–0.125) |
| Rendering + packing (ms/frame) | 5.032 (5.007–5.051) | 3.918 (3.828–3.997) |
| PNG encoding (ms/frame) | 3.508 (3.440–3.543) | 0.616 (0.610–0.627) |
| Renderer process to first frame (ms) | 9.231 (8.573–9.512) | 37.637 (37.070–38.325) |
| Peak RSS for steady-state batch (MiB) | 14.906 (14.688–15.766) | 30.891 (30.781–32.266) |

## Every layout

| Layout | Go render (ms) | Python render (ms) | Go render + pack (ms) | Python render + pack (ms) | Go pipeline time vs Python |
| --- | ---: | ---: | ---: | ---: | ---: |
| Default | 3.285 | 3.754 | 5.032 | 3.918 | 1.28× |
| Devices | 3.404 | 4.542 | 5.159 | 4.709 | 1.10× |
| Usage | 3.312 | 4.431 | 5.048 | 4.559 | 1.11× |
| Music | 3.225 | 3.619 | 4.978 | 3.724 | 1.34× |
| Antigravity | 3.333 | 4.198 | 5.091 | 4.391 | 1.16× |

## Interpretation

Across the five layouts, Go takes 11–25% less time to render, but 10–34% more time to render and prepare a fresh display buffer. Its median peak RSS is 14.7–15.9 MiB versus 30.9–31.1 MiB for the Python renderer harness. Fresh renderer processes finish in 9.1–9.8 ms versus 37.6–44.3 ms.

Go's current `display.Pack` walks every pixel through the `image.Image` interface and calculates luminance. The original Pillow renderer already creates a packed 1-bit image and exports its bytes through native code. Packing is about 14× slower in Go in this test. A specialized path for the renderer's `*image.Gray` output is a concrete candidate for a separate optimization, followed by equivalence tests and another benchmark.

PNG encoding is also slower in Go here, but the formats differ: 8-bit grayscale versus 1-bit monochrome. Python did not encode PNGs in its original production display loop. These numbers should not be presented as an identical PNG codec comparison.

## Limits and reproduction

Layouts are semantically comparable, not pixel-identical. Each implementation retains its actual fonts, rasterization, labels, text fitting, chart scaling, and window presentation. The music scenario has no downloaded cover image. The harness excludes API collectors, account logins, GPIO/SPI transfers, panel BUSY waits, and daemon idle periods. Startup and peak memory are renderer-harness measurements, not full-application measurements. Filesystem caches are warm; CPU frequency, background load, and thermals are uncontrolled. Results do not establish Raspberry Pi performance, energy use, or end-to-end refresh latency.

No production optimization was made for this comparison. See [methodology and rerun instructions](README.md), [raw samples and metadata](results/2026-10-15-macos-arm64.json), and generated preview pairs under `dist/benchmarks/`.

```sh
python3 benchmarks/run.py --iterations 100 --repeats 5 --startups 10
```
