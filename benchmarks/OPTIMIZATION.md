# Grayscale packing optimization — 15 October 2026

The production Go framebuffer packer now reads `image.Gray` pixels directly and packs eight pixels into each output byte. It preserves the 128 grayscale cutoff, MSB-first bit order, row stride, and image bounds. Other image types continue through the existing generic conversion path. No unsafe code, architecture-specific instructions, or benchmark-only packing path is used.

On the Apple M5 Pro, packing takes about 18× less time and complete render-and-pack time falls by approximately one third. Go now takes 12–26% less time than Python/Pillow for render-and-pack across all five layouts.

## Default layout

| Measurement | Go before | Go optimized | Python rerun |
| --- | ---: | ---: | ---: |
| Render (ms) | 3.285 (3.253–3.300) | 3.255 (3.192–3.284) | 3.832 (3.720–4.059) |
| Pack (ms) | 1.743 (1.733–1.747) | 0.096 (0.094–0.098) | 0.122 (0.121–0.124) |
| Render + pack (ms) | 5.032 (5.007–5.051) | 3.360 (3.285–3.603) | 3.867 (3.829–3.916) |
| PNG encode (ms) | 3.508 (3.440–3.543) | 3.461 (3.414–3.488) | 0.612 (0.594–0.618) |
| Renderer process to first frame (ms) | 9.231 (8.573–9.512) | 7.386 (6.778–7.736) | 37.061 (36.500–40.048) |
| Steady-state peak RSS (MiB) | 14.906 (14.688–15.766) | 15.000 (14.781–15.719) | 30.984 (30.828–32.953) |

Values are medians of batch means; parentheses show the minimum and maximum. Startup uses individual fresh-process observations. Lower is better. The unoptimized Go measurements come from the preceding baseline run; optimized Go and Python were interleaved in the new run.

## All layouts

| Layout | Packing speedup vs Go baseline | Go render + pack before (ms) | Go render + pack after (ms) | Python render + pack rerun (ms) | Go time reduction vs Python |
| --- | ---: | ---: | ---: | ---: | ---: |
| Default | 18.1× | 5.032 | 3.360 | 3.867 | 13.1% |
| Devices | 18.2× | 5.159 | 3.452 | 4.674 | 26.1% |
| Usage | 18.1× | 5.048 | 3.371 | 4.508 | 25.2% |
| Music | 18.3× | 4.978 | 3.274 | 3.722 | 12.0% |
| Antigravity | 17.9× | 5.091 | 3.415 | 4.310 | 20.8% |

## Validation

- Byte-for-byte comparison against the original generic packer for black, white, all 256 grayscale values, and seeded random frames with positive/negative origins and padded subimage strides. Source pixels remain unchanged.
- Every output-byte bit pattern checked using grayscale 127 and 128 at the threshold. Existing split-controller boundary and partial-refresh tests pass.
- All ten Go/Python preview PNGs and the shared fixture are byte-identical to the baseline artifacts.
- `go test -race ./...`, `go vet ./...`, and `make package` pass, including Linux ARMv6 and ARM64 builds.
- Isolated `BenchmarkPackGray` ran three times at 76.4–78.1 µs/op, with one allocation per frame (81,920 allocated bytes). The full application-style benchmark reports approximately 95–97 µs per pack; different harnesses and garbage-collection activity produce different timings. The comparison tables consistently use the full harness.

## Method and limits

Same Apple M5 Pro, 64 GiB RAM, macOS 26.6.2; Go 1.26.7, Python 3.12.14, Pillow 12.3.0. Same shared fixture, ten warm-up frames, five batches of 100 iterations per operation/layout/implementation, and ten startup samples. Runtime settings and Python code were unchanged. The optimized result records the base Go commit plus the exact uncommitted production diff and harness hashes.

These remain renderer-harness measurements on a Mac. No APIs, physical panel, GPIO/SPI transfers, or Raspberry Pi hardware were measured. PNG formats still differ (Go grayscale versus Python monochrome), and PNG encoding was not optimized. Memory is peak RSS during the benchmark batch, not daemon idle usage. See the [full methodology](README.md).

[Baseline report](RESULTS.md) · [Baseline raw samples](results/2026-10-15-macos-arm64.json) · [Optimized raw samples](results/2026-10-15-macos-arm64-optimized.json)

```sh
python3 benchmarks/run.py --out dist/benchmarks-optimized --iterations 100 --repeats 5 --startups 10
go test ./internal/display -run '^$' -bench BenchmarkPackGray -benchmem -count 3
```
