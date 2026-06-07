#!/usr/bin/env python3
"""
HelloCompose — client service

Polls the API until it is ready, prints the response (including GPU and
Wendy identity info), then exits.
"""

import json
import os
import sys
import time
import urllib.error
import urllib.request

API = "http://localhost:8080"

WENDY_APP_ID = os.environ.get("WENDY_APP_ID", "not set")
WENDY_HOSTNAME = os.environ.get("WENDY_HOSTNAME", "not set")
WENDY_DEVICE_HOSTNAME = os.environ.get("WENDY_DEVICE_HOSTNAME", "not set")

print(f"[client] WENDY_APP_ID          = {WENDY_APP_ID}", flush=True)
print(f"[client] WENDY_HOSTNAME        = {WENDY_HOSTNAME}", flush=True)
print(f"[client] WENDY_DEVICE_HOSTNAME = {WENDY_DEVICE_HOSTNAME}", flush=True)
print(f"[client] Waiting for api at {API}...", flush=True)

for attempt in range(30):
    try:
        with urllib.request.urlopen(API, timeout=2) as resp:
            data = json.loads(resp.read())
        print(f"[client] {data['message']}", flush=True)
        print(f"[client] API machine: {data['machine']}  python: {data['python']}", flush=True)
        print(f"[client] API GPU:     {data['gpu']}", flush=True)
        wendy = data.get("wendy", {})
        print(f"[client] API WENDY_APP_ID:    {wendy.get('app_id')}", flush=True)
        print(f"[client] API WENDY_HOSTNAME:  {wendy.get('hostname')}", flush=True)
        print(f"[client] Note: {data['note']}", flush=True)
        print("[client] Done.", flush=True)
        sys.exit(0)
    except (urllib.error.URLError, OSError):
        time.sleep(1)

print("[client] Timed out waiting for api", file=sys.stderr, flush=True)
sys.exit(1)
