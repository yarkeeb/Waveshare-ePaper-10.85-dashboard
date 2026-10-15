# Offline Go/Python benchmark

Recorded results: [original baseline](RESULTS.md) and [optimized grayscale packing](OPTIMIZATION.md).

Run from the repository with Go and a Python interpreter that has Pillow installed:

```sh
python3 benchmarks/run.py --iterations 100 --repeats 5 --startups 10
```

The runner builds the current Go renderer, extracts the unchanged Python drawing functions and `EPD.getbuffer` from commit `ceced5eb8a0c17c58d73064a687f924bc3e3b461`, and restores its fonts/icons under `dist/benchmarks/original-assets`. No Python application startup code, authentication, network collectors, or GPIO/SPI operations execute. Nothing is downloaded; the original commit must be present locally. The Python harness is only a benchmark dependency, not part of deployment.

Both implementations receive the same fixed timestamp, weather, histories, usage, device status, and music metadata through one JSON fixture. The five layouts are default, devices (Strava/Bambu/Roborock), usage (Claude/Codex), music, and Antigravity. Music uses the fallback icon; downloaded album-art decoding is not covered. The existing renderers have different labels, layout, text sizing, chart scaling, and quota-window presentation, so outputs are semantically comparable but not pixel-identical. Preview PNGs are saved for inspection.

Measurements:

- **Render:** existing renderer and snapshot/copy path, after ten warm-up frames; no encoding, file writes, or framebuffer packing.
- **Pack:** the existing display packing function on a previously rendered frame. Python already has a packed 1-bit Pillow image; Go uses an 8-bit grayscale image. Both produce 81,600 bytes.
- **Render + pack:** sequential rendering and preparation of a fresh display buffer, the CPU work before sending a frame to hardware.
- **PNG:** each implementation's default PNG encoder, writing to a discard sink. Python encodes 1-bit pixels; Go encodes 8-bit grayscale pixels. This is an output-path measurement, not an identical codec workload. PNG output was not part of the original Python production loop.
- **Renderer process to first frame:** process launch through imports, fixture/font setup, one render and pack, JSON output and exit. This is warm-filesystem startup of the harness, not full application startup or a reboot/cold-disk test. Python imports only renderer dependencies; full original application memory/startup would include additional libraries.
- **Peak RSS:** OS-reported child-process high-water resident memory for the complete steady-state batch, including warm-up and PNG encoding. It includes native allocations, unlike Python-only allocation profilers. It is not idle daemon memory.

Runs are sequential, with reproducibly shuffled implementation/layout order in each round. Each timing sample is the mean of the requested iteration count; summaries report the median and range of those batch means, not per-frame percentiles. Startup is measured in separate short-lived processes. Normal Go/Python garbage collection remains enabled; the harness uses production implementations without benchmark-specific fast paths or unusual runtime configuration. CPU frequency, system load, filesystem caches, and thermal state are not pinned.

`results.json` contains raw samples, summaries, source revisions, runtime versions, host information, fixture hash, harness hashes, and any working-tree production diff. All generated artifacts stay in ignored `dist/benchmarks/`. The harness uses Unix `wait4` (macOS/Linux); macOS RSS bytes and Linux RSS kilobytes are normalized to MiB.

Results on a development Mac do not predict Raspberry Pi latency, idle power, network performance, device refresh duration, or end-to-end refresh time. Repeat on the target Pi when available.
