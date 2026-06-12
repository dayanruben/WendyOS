#!/usr/bin/env python3
"""
ROS2 — listener service

Both talker and listener run with isolation: shared-network so they share
a network namespace — DDS multicast discovery works across them without a
bridge or host networking.

`frameworks.ros2` is declared per-service in wendy.json; Wendy injects
ROS_DOMAIN_ID and RMW_IMPLEMENTATION into each container independently
(WDY-880, PR #897).

This example reads from /dev/shm to demonstrate shared-network without
requiring a full ROS2 install.  On a real ROS2 image rclpy would subscribe
to the topic normally.
"""

import os
import sys
import time

domain_id = os.environ.get("ROS_DOMAIN_ID")
rmw = os.environ.get("RMW_IMPLEMENTATION")

print(f"[listener] ROS_DOMAIN_ID      = {domain_id or '<not set>'}", flush=True)
print(f"[listener] RMW_IMPLEMENTATION = {rmw or '<not set>'}", flush=True)
print(f"[listener] WENDY_HOSTNAME     = {os.environ.get('WENDY_HOSTNAME', '<not set>')}", flush=True)

if domain_id != "42":
    print(f"[listener] ✗ expected ROS_DOMAIN_ID=42, got {domain_id!r}", flush=True)
    sys.exit(1)
if rmw != "rmw_cyclonedds_cpp":
    print(f"[listener] ✗ expected RMW_IMPLEMENTATION=rmw_cyclonedds_cpp, got {rmw!r}", flush=True)
    sys.exit(1)

print("[listener] ✓ ROS2 env vars injected by Wendy from frameworks.ros2 config", flush=True)

SHM = "/dev/shm/ros2-channel"
print(f"[listener] waiting for talker messages on {SHM} ...", flush=True)

# Fail loudly if the talker never shows up (broken deploy), then listen until
# the app is stopped so the group keeps running for live inspection with
# `wendy device ros2 ...`.
STARTUP_TIMEOUT_S = 120
start = time.monotonic()
last = None
received = 0
while True:
    try:
        with open(SHM) as f:
            val = f.read().strip()
        if val != last:
            last = val
            received += 1
            if received == 1:
                print("[listener] ✓ receiving messages — shared-network isolation works", flush=True)
            # Log every 10th message after the first ten so the stream stays readable.
            if received <= 10 or received % 10 == 0:
                print(f"[listener] received seq #{val}", flush=True)
    except FileNotFoundError:
        if received == 0 and time.monotonic() - start > STARTUP_TIMEOUT_S:
            print("[listener] ✗ timed out waiting for talker", flush=True)
            sys.exit(1)
    time.sleep(0.5)
