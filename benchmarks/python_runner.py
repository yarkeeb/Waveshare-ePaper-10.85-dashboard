"""Offline harness for unchanged rendering functions extracted from upstream."""
import argparse
import calendar
import io
import json
import math
import os
import threading
import time
from collections import deque
from datetime import datetime, timezone
from pathlib import Path
from types import SimpleNamespace

from PIL import Image, ImageDraw, ImageFont, ImageOps


class FixedDatetime(datetime):
    @classmethod
    def now(cls, tz=None):
        value = cls(2026, 10, 15, 14, 35, tzinfo=timezone.utc)
        return value.astimezone(tz) if tz else value.replace(tzinfo=None)


class NullWriter:
    def write(self, data):
        return len(data)
    def flush(self):
        pass


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', required=True)
    parser.add_argument('--assets', required=True)
    parser.add_argument('--fixture', required=True)
    parser.add_argument('--scenario', default='default')
    parser.add_argument('--n', type=int, default=100)
    parser.add_argument('--preview', default='')
    args = parser.parse_args()
    # Source extraction happens in the orchestrator, outside process timings.
    scope = dict(globals(), datetime=FixedDatetime, icon_cache={},
                 ICON_DIR=str(Path(args.assets) / 'icons'))
    for name in ['STRAVA', 'BAMBU', 'ROBOROCK', 'CLAUDE', 'CODEX', 'SPOTIFY', 'ANTIGRAVITY']:
        scope['ENABLE_' + name] = False
    layouts = {'default': [], 'devices': ['STRAVA', 'BAMBU', 'ROBOROCK'],
               'usage': ['CLAUDE', 'CODEX'], 'music': ['SPOTIFY'], 'antigravity': ['ANTIGRAVITY']}
    for name in layouts[args.scenario]:
        scope['ENABLE_' + name] = True
    exec(compile(Path(args.source).read_text(), args.source, 'exec'), scope)
    s = scope['DataStore']()
    scope['data_store'] = s
    f = json.loads(Path(args.fixture).read_text())
    w = f['weather']
    s.weather = {'current': dict(zip(
        ['temperature_2m', 'relative_humidity_2m', 'surface_pressure', 'wind_speed_10m',
         'wind_direction_10m', 'uv_index', 'weather_code', 'is_day'],
        [w[k] for k in ['Temperature', 'Humidity', 'Pressure', 'WindSpeed', 'WindDirection', 'UV', 'Code', 'Day']])),
        'hourly': {'time': [p['Time'][:16] for p in w['Forecast']],
                   'temperature_2m': [p['Temperature'] for p in w['Forecast']],
                   'weather_code': [p['Code'] for p in w['Forecast']]}}
    s.aqi = f['aqi']; s.gmail_unread = f['gmail']
    s.sysload = {'cpu': f['system']['Load'], 'ram_free': f['system']['FreeMB'], 'history': f['system']['History']}
    s.crypto = {'btc': f['crypto']['BTC'][-1], 'eth': f['crypto']['ETH'][-1],
                'btc_hist': f['crypto']['BTC'], 'eth_hist': f['crypto']['ETH']}
    s.ping = {'current': f['ping']['Milliseconds'], 'history': f['ping']['History']}
    a = f['strava']
    s.strava = dict(zip(['rides', 'total_distance', 'distance_curr', 'distance_prev', 'bike_total', 'hike_total'],
                       [a[k] for k in ['Rides', 'Distance', 'CurrentDistance', 'PreviousDistance', 'Bike', 'Hike']]))
    p = f['bambu']
    s.printer = {'status': p['Status'], 'percentage': p['Percent'], 'remaining_time': p['Remaining'],
                 'layers': f"{p['Layer']}/{p['Layers']}"}
    v = f['roborock']
    s.roborock = dict(zip(['status', 'battery', 'is_cleaning', 'current_area', 'ref_area', 'pct', 'last_date'],
                         [v[k] for k in ['Status', 'Battery', 'Cleaning', 'Area', 'Reference', 'Percent', 'Last']]))
    for name in ['claude', 'codex']:
        setattr(s, name, {key: {'utilization': window['Used'], 'resets_at': window['Reset']}
                         for key, window in zip(['five_hour', 'seven_day'], f[name]['Windows'])})
    s.antigravity = {'models': [{'modelId': key, 'usedPercentage': window['Used'], 'resetDate': window['Reset']}
                                for key, window in zip(['claude-opus-4-6-thinking', 'gemini-3-pro-high'], f['antigravity']['Windows'])]}
    s.spotify = {'status': 'PLAYING', 'text': f"{f['spotify']['Artist']} - {f['spotify']['Track']}", 'cover': None}
    fonts = {str(size): ImageFont.truetype(str(Path(args.assets) / 'fnt/Aldrich-Regular.ttc'), size)
             for size in [20, 24, 28, 32, 35, 40, 60, 80]}
    fonts['clock'] = ImageFont.truetype(str(Path(args.assets) / 'fnt/advanced_led_board-7.ttc'), 180)
    panel = SimpleNamespace(width=1360, height=480)
    img = None
    def draw():
        nonlocal img
        img = scope['render_screen'](panel, fonts)
    def pack():
        data = scope['getbuffer'](panel, img)
        assert len(data) == 81600
    def encode():
        img.save(NullWriter(), format='PNG')
    def pipeline():
        draw(); pack()
    pipeline()
    if args.preview:
        img.save(args.preview)
    result = {}
    if args.n > 0:
        for _ in range(10):
            pipeline(); encode()
        for name, fn in [('render_ms', draw), ('pack_ms', pack), ('png_ms', encode), ('render_pack_ms', pipeline)]:
            start = time.perf_counter_ns()
            for _ in range(args.n):
                fn()
            result[name] = (time.perf_counter_ns() - start) / args.n / 1e6
    print(json.dumps(result))


if __name__ == '__main__':
    main()
