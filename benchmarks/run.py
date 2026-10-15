#!/usr/bin/env python3
"""Reproducible offline Go/Python comparison; requires Go, Git and Pillow."""
import argparse
import ast
import hashlib
import json
import os
import platform
import random
import statistics
import subprocess
import sys
import tempfile
import textwrap
import time
from pathlib import Path

import PIL

ROOT = Path(__file__).resolve().parents[1]
BASELINE = 'ceced5eb8a0c17c58d73064a687f924bc3e3b461'
SCENARIOS = ['default', 'devices', 'usage', 'music', 'antigravity']


def git_file(path):
    return subprocess.check_output(['git', 'show', f'{BASELINE}:{path}'], cwd=ROOT)


def prepare(out):
    assets = out / 'original-assets'
    for path in ['fnt/Aldrich-Regular.ttc', 'fnt/advanced_led_board-7.ttc']:
        target = assets / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(git_file(path))
    icon_paths = subprocess.check_output(['git', 'ls-tree', '-r', '--name-only', BASELINE, 'icons'], cwd=ROOT, text=True).splitlines()
    for path in icon_paths:
        target = assets / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(git_file(path))
    source = git_file('main.py').decode().replace('\r\n', '\n')
    wanted = {'DataStore', 'get_cached_icon', 'time_until', 'draw_icon', 'draw_sparkline', 'get_weather_icon', 'render_screen'}
    nodes = [node for node in ast.parse(source).body if isinstance(node, (ast.ClassDef, ast.FunctionDef)) and node.name in wanted]
    assert {node.name for node in nodes} == wanted
    extracted = [ast.get_source_segment(source, node) for node in nodes]
    driver = git_file('lib/waveshare_epd/epd10in85.py').decode().replace('\r\n', '\n')
    epd = next(node for node in ast.parse(driver).body if isinstance(node, ast.ClassDef) and node.name == 'EPD')
    method = next(node for node in epd.body if isinstance(node, ast.FunctionDef) and node.name == 'getbuffer')
    extracted.append(textwrap.dedent('\n'.join(driver.splitlines()[method.lineno-1:method.end_lineno])))
    (out / 'original_renderer.py').write_text('\n\n'.join(extracted) + '\n')
    fixture = {
        'weather': {'Temperature': 21, 'Humidity': 62, 'Pressure': 1018, 'WindSpeed': 12,
                    'WindDirection': 45, 'UV': 3, 'Code': 2, 'Day': True,
                    'Forecast': [{'Time': f'2026-10-15T{h}:00:00Z', 'Temperature': 36-h, 'Code': h % 4, 'Rain': 0} for h in range(15, 19)]},
        'aqi': 34, 'gmail': 7,
        'system': {'Load': 0.32, 'FreeMB': 287, 'History': [0.1, 0.2, 0.4, 0.3, 0.3, 0.2, 0.5, 0.1, 0.4, 0.2] * 3},
        'ping': {'Milliseconds': 24, 'History': [22, 28, 24, 20, 23, 34, 28, 25, 21, 24] * 5},
        'crypto': {'BTC': [65000, 67000, 64000, 68000, 70000, 69000, 68000, 67500, 68200, 69000] * 5,
                   'ETH': [2300, 2400, 2250, 2500, 2450, 2600, 2510, 2490, 2520, 2600] * 5},
        'strava': {'Rides': 142, 'Distance': 5840, 'CurrentDistance': 1430, 'PreviousDistance': 2190, 'Bike': 5840, 'Hike': 218},
        'bambu': {'Status': 'RUNNING', 'Percent': 64, 'Remaining': 42, 'Layer': 128, 'Layers': 200},
        'roborock': {'Status': 'Clean', 'Battery': 82, 'Cleaning': True, 'Area': 24.5, 'Reference': 60, 'Percent': 41, 'Last': '2026-10-14'},
        'spotify': {'Playing': True, 'Artist': 'Demo artist', 'Track': 'A quiet morning', 'Cover': None},
    }
    for name in ['claude', 'codex', 'antigravity']:
        fixture[name] = {'Windows': [{'Name': '5 hours', 'Used': 32, 'Reset': '2026-10-15T16:35:00Z'},
                                    {'Name': '7 days', 'Used': 58, 'Reset': '2026-10-18T14:35:00Z'}]}
    (out / 'fixture.json').write_text(json.dumps(fixture, indent=2) + '\n')
    subprocess.run(['go', 'build', '-trimpath', '-o', str(out / 'go-renderer'), './benchmarks/go'], cwd=ROOT, check=True)


def run_child(command):
    # wait4 provides per-child high-water RSS, including native Pillow/Go memory.
    # File-backed stdout avoids a pipe deadlock and keeps output out of timings.
    with tempfile.TemporaryFile() as stdout, tempfile.TemporaryFile() as stderr:
        started = time.perf_counter_ns()
        child = subprocess.Popen(command, stdout=stdout, stderr=stderr, cwd=ROOT)
        _, status, usage = os.wait4(child.pid, 0)
        elapsed = (time.perf_counter_ns() - started) / 1e6
        child.returncode = os.waitstatus_to_exitcode(status)
        stdout.seek(0); stderr.seek(0)
        if child.returncode:
            raise RuntimeError(stderr.read().decode())
        result = json.loads(stdout.read())
    result['process_ms'] = elapsed
    result['peak_rss_mib'] = usage.ru_maxrss / (1024**2 if sys.platform == 'darwin' else 1024)
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--out', default='dist/benchmarks')
    parser.add_argument('--iterations', type=int, default=100)
    parser.add_argument('--repeats', type=int, default=5)
    parser.add_argument('--startups', type=int, default=10)
    args = parser.parse_args()
    if min(args.iterations, args.repeats, args.startups) < 1:
        parser.error('counts must be positive')
    out = (ROOT / args.out).resolve(); out.mkdir(parents=True, exist_ok=True)
    prepare(out)
    commands = {
        'go': [str(out / 'go-renderer')],
        'python': [sys.executable, str(ROOT / 'benchmarks/python_runner.py'), '--source', str(out / 'original_renderer.py'), '--assets', str(out / 'original-assets')],
    }
    def command(impl, scenario, n):
        # Go's flag package accepts both one- and two-dash flags.
        return commands[impl] + ['--fixture', str(out / 'fixture.json'), '--scenario', scenario, '--n', str(n)]
    # Previews and sanity checks are outside measurement runs.
    from PIL import Image
    for scenario in SCENARIOS:
        for impl in commands:
            path = out / f'{impl}-{scenario}.png'
            run_child(command(impl, scenario, 0) + ['--preview', str(path)])
            with Image.open(path) as img:
                assert img.size == (1360, 480)
                extrema = img.convert('L').getextrema()
                assert extrema == (0, 255), (path, extrema)
    rows = []
    rng = random.Random(20261015)
    for kind, repeats, n in [('steady', args.repeats, args.iterations), ('startup', args.startups, 0)]:
        for repeat in range(repeats):
            pairs = [(scenario, impl) for scenario in SCENARIOS for impl in commands]
            rng.shuffle(pairs)
            for scenario, impl in pairs:
                result = run_child(command(impl, scenario, n))
                rows.append(dict(kind=kind, repeat=repeat, scenario=scenario, implementation=impl, **result))
            print(f'{kind} round {repeat + 1}/{repeats} complete', flush=True)
    metadata = {'platform': platform.platform(), 'python': sys.version, 'pillow': PIL.__version__,
                'go': subprocess.check_output(['go', 'version'], text=True).strip(),
                'go_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
                'python_commit': BASELINE, 'iterations': args.iterations, 'repeats': args.repeats, 'startups': args.startups,
                'fixture_sha256': hashlib.sha256((out / 'fixture.json').read_bytes()).hexdigest()}
    metadata['go_working_diff'] = subprocess.check_output(
        ['git', 'diff', 'HEAD', '--', 'internal', 'assets.go', 'go.mod', 'go.sum'], cwd=ROOT, text=True)
    metadata['harness_sha256'] = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest()
                                for path in [ROOT / 'benchmarks/run.py', ROOT / 'benchmarks/python_runner.py', ROOT / 'benchmarks/go/main.go']}
    if sys.platform == 'darwin':
        metadata['cpu'] = subprocess.check_output(['sysctl', '-n', 'machdep.cpu.brand_string'], text=True).strip()
        metadata['ram_bytes'] = int(subprocess.check_output(['sysctl', '-n', 'hw.memsize'], text=True))
    result = {'metadata': metadata, 'samples': rows, 'summary': {}}
    for scenario in SCENARIOS:
        result['summary'][scenario] = {}
        for impl in commands:
            measures = {}
            for kind, keys in [('steady', ['render_ms', 'pack_ms', 'png_ms', 'render_pack_ms', 'peak_rss_mib']), ('startup', ['process_ms'])]:
                selected = [r for r in rows if r['scenario'] == scenario and r['implementation'] == impl and r['kind'] == kind]
                for key in keys:
                    values = [r[key] for r in selected]
                    measures[key] = {'median': statistics.median(values), 'min': min(values), 'max': max(values)}
            result['summary'][scenario][impl] = measures
    (out / 'results.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result['summary'], indent=2), flush=True)
    print(f'Results saved to {out / "results.json"}')


if __name__ == '__main__':
    main()
