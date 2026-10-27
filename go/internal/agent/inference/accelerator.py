"""Engines that score the YOLO ONNX backend: the CPU, and TensorRT on an NVIDIA GPU.

The agent decides which engines to offer, in accelerator_plan on the config
line, and pins the TensorRT Python bindings it offers. This module decides
what actually runs and reports it. Its report, never the agent's plan, is what
inference_status shows.

With accelerator auto the worker scores on the CPU from the start. A TensorRT
engine is found in the cache or built by a child process in the background,
compared with the CPU on the same frames, and only then swapped in. Every way
of not getting there leaves the CPU running and says why.

Run as a script with --build-engine, this module is that child process.
"""

import ctypes
import datetime
import fcntl
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import time
from collections import deque

# Bump only on purpose: every cached engine is rebuilt when it changes.
SCHEMA = 1
# FP16 halves the engine and runs on the Orin's tensor cores. The parity check
# below decides whether the scores are close enough to the CPU's FP32.
PRECISION = "fp16"
# TensorRT's default, named so that a change to it changes the cache key.
OPTIMIZATION_LEVEL = 3

# Parity tolerances. Each CPU detection must have a TensorRT detection of the
# same label overlapping it by IoU >= PARITY_IOU, with scores within
# PARITY_SCORE, and the other way round. Detections within PARITY_SCORE of the
# threshold may appear on one side only, since FP16 can move them across it.
# IoU 0.8 rather than tighter because FP16 moves a box edge by a fraction of a
# pixel, which costs a small, distant person's box several points of IoU; an
# engine that is wrong, rather than rounded, misses by far more.
PARITY_IOU = 0.8
PARITY_SCORE = 0.05
# And no class score anywhere in the raw output, detected or not, may differ by
# more than PARITY_RAW_SCORE. This catches an engine that is wrong on an empty
# scene, where there are no detections to compare.
PARITY_RAW_SCORE = 0.1
# Recent camera frames compared, besides one seeded noise frame.
PARITY_FRAMES = 3

# How long to wait before checking free memory again, for a build or a load.
MEMORY_RETRY_SECONDS = 120
# A failed build is not retried for the same cache key for this long, so a
# model TensorRT cannot handle costs one build a day rather than one a start.
TOMBSTONE_SECONDS = 24 * 3600
# Status and throughput go to the agent at least this often.
STATUS_SECONDS = 10

MAX_PLAN_BYTES = 1 << 30

# What the child reports when a build had to wait for memory.
EXIT_WAITING_FOR_MEMORY = 3


class EngineError(Exception):
    """An engine could not be loaded, built or run."""


def utc_now():
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def available_mib():
    """MemAvailable in MiB, or None off Linux. The Jetson's GPU shares this
    memory, so it is also the GPU's free memory."""
    try:
        with open("/proc/meminfo") as meminfo:
            for line in meminfo:
                if line.startswith("MemAvailable:"):
                    return int(line.split()[1]) // 1024
    except OSError:
        return None
    return None


def short(text, limit=300):
    text = str(text)
    return text if len(text) <= limit else text[:limit - 3] + "..."


# ---------------------------------------------------------------------------
# Engine cache


def key_fields(model_sha256, tensorrt_version, library_version, cuda_version, gpu_arch,
               input_shape, output_shape, workspace_mib):
    """Everything an engine depends on. Two engines with equal fields are
    interchangeable; any difference means a rebuild."""
    return {
        "schema": SCHEMA,
        "model_sha256": model_sha256,
        "tensorrt": tensorrt_version,
        "libnvinfer": library_version,
        "cuda": cuda_version,
        "gpu_arch": gpu_arch,
        "precision": PRECISION,
        "input_shape": [int(n) for n in input_shape],
        "output_shape": [int(n) for n in output_shape],
        "builder": {"optimization_level": OPTIMIZATION_LEVEL, "workspace_mib": int(workspace_mib)},
    }


def cache_key(fields):
    canonical = json.dumps(fields, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(canonical.encode()).hexdigest()


def _fsync_directory(path):
    descriptor = os.open(path, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def _write_synced(path, payload):
    with open(path, "wb") as handle:
        handle.write(payload)
        handle.flush()
        os.fsync(handle.fileno())


class EngineCache:
    """engines/<key[:32]>/{engine.plan,engine.json} under the agent's inference
    directory, which only root can read or write.

    Nothing is loaded on trust. A lookup recomputes the key, requires every
    field of the record to equal it, and hashes the plan against the record.
    Only a builder holding the build lock writes, into a private temporary
    directory that is renamed into place after the plan deserialized, so a
    partial build is never visible."""

    def __init__(self, root):
        self.root = root

    def directory(self, key):
        return os.path.join(self.root, key[:32])

    def lookup(self, key, fields):
        """(plan bytes, record, None) for a verified engine, else
        (None, None, why not). A missing engine is not a problem: why is None."""
        directory = self.directory(key)
        try:
            with open(os.path.join(directory, "engine.json"), "rb") as handle:
                record = json.loads(handle.read(1 << 20))
        except FileNotFoundError:
            return None, None, None
        except (OSError, ValueError) as exc:
            return None, None, f"engine record is unreadable ({type(exc).__name__})"
        if not isinstance(record, dict) or record.get("key") != key or record.get("fields") != fields:
            return None, None, "engine record does not match this model, TensorRT and GPU"
        try:
            with open(os.path.join(directory, "engine.plan"), "rb") as handle:
                plan = handle.read(MAX_PLAN_BYTES + 1)
        except OSError as exc:
            return None, None, f"engine plan is unreadable ({type(exc).__name__})"
        if len(plan) != record.get("plan_bytes") or hashlib.sha256(plan).hexdigest() != record.get("plan_sha256"):
            return None, None, "engine plan does not match its record"
        return plan, record, None

    def store(self, key, fields, plan, build_seconds):
        """Promotes a built plan. Call only while holding the build lock."""
        record = {"key": key, "fields": fields, "plan_sha256": hashlib.sha256(plan).hexdigest(),
                  "plan_bytes": len(plan), "built_at": utc_now(), "build_seconds": round(build_seconds, 1)}
        temporary = tempfile.mkdtemp(prefix=".tmp-", dir=self.root)
        try:
            _write_synced(os.path.join(temporary, "engine.plan"), plan)
            _write_synced(os.path.join(temporary, "engine.json"), json.dumps(record, indent=1).encode())
            _fsync_directory(temporary)
            final = self.directory(key)
            if os.path.lexists(final):
                shutil.rmtree(final)
            os.rename(temporary, final)
            _fsync_directory(self.root)
        except BaseException:
            shutil.rmtree(temporary, ignore_errors=True)
            raise
        return record

    def discard(self, key):
        """Removes an engine. Call only while holding the build lock."""
        shutil.rmtree(self.directory(key), ignore_errors=True)

    def remove_partial_builds(self):
        """Removes temporary directories of builders that died. Call only while
        holding the build lock: nobody else is writing one."""
        for name in os.listdir(self.root):
            if name.startswith(".tmp-"):
                shutil.rmtree(os.path.join(self.root, name), ignore_errors=True)

    def tombstone_path(self, key):
        return self.directory(key) + ".failed.json"

    def tombstone(self, key, error, now=None):
        payload = json.dumps({"key": key, "error": short(error, 500),
                              "failed_at": time.time() if now is None else now}).encode()
        descriptor, temporary = tempfile.mkstemp(prefix=".tmp-failed-", dir=self.root)
        try:
            with os.fdopen(descriptor, "wb") as handle:
                handle.write(payload)
            os.replace(temporary, self.tombstone_path(key))
        except BaseException:
            try:
                os.unlink(temporary)
            except OSError:
                pass
            raise

    def failed(self, key, now=None):
        """Why the last build of key failed, while that failure still holds."""
        try:
            with open(self.tombstone_path(key), "rb") as handle:
                record = json.loads(handle.read(1 << 16))
        except (OSError, ValueError):
            return None
        now = time.time() if now is None else now
        if not isinstance(record, dict) or record.get("key") != key:
            return None
        failed_at = record.get("failed_at")
        if not isinstance(failed_at, (int, float)) or not 0 <= now - failed_at < TOMBSTONE_SECONDS:
            return None
        hours = max(1, round((TOMBSTONE_SECONDS - (now - failed_at)) / 3600))
        return f"the engine build failed ({record.get('error')}); it is retried in about {hours}h or when the model changes"


# ---------------------------------------------------------------------------
# CUDA and TensorRT


class Cuda:
    """The few CUDA runtime calls an engine needs, through ctypes on the
    image's libcudart: the library TensorRT itself links, so no CUDA wheel."""

    MEMCPY_HOST_TO_DEVICE = 1
    MEMCPY_DEVICE_TO_HOST = 2
    STREAM_NON_BLOCKING = 1
    ATTRIBUTE_CAPABILITY_MAJOR = 75
    ATTRIBUTE_CAPABILITY_MINOR = 76

    def __init__(self, library):
        c_int, c_void, c_size = ctypes.c_int, ctypes.c_void_p, ctypes.c_size_t
        pointer = ctypes.POINTER
        self.lib = ctypes.CDLL(library)
        for name, restype, argtypes in (
            ("cudaGetErrorString", ctypes.c_char_p, [c_int]),
            ("cudaRuntimeGetVersion", c_int, [pointer(c_int)]),
            ("cudaSetDevice", c_int, [c_int]),
            ("cudaDeviceGetAttribute", c_int, [pointer(c_int), c_int, c_int]),
            ("cudaMalloc", c_int, [pointer(c_void), c_size]),
            ("cudaFree", c_int, [c_void]),
            ("cudaMemcpyAsync", c_int, [c_void, c_void, c_size, c_int, c_void]),
            ("cudaDeviceGetStreamPriorityRange", c_int, [pointer(c_int), pointer(c_int)]),
            ("cudaStreamCreateWithPriority", c_int, [pointer(c_void), ctypes.c_uint, c_int]),
            ("cudaStreamSynchronize", c_int, [c_void]),
            ("cudaStreamDestroy", c_int, [c_void]),
            ("cudaDeviceReset", c_int, []),
        ):
            function = getattr(self.lib, name)
            function.restype, function.argtypes = restype, argtypes
        # Creates the process's CUDA context: a wedged GPU fails here, before
        # anything is built.
        self.check(self.lib.cudaSetDevice(0), "cudaSetDevice")

    def check(self, code, call):
        if code:
            message = self.lib.cudaGetErrorString(code)
            raise EngineError(f"{call} failed: {message.decode(errors='replace') if message else code} ({code})")

    def runtime_version(self):
        version = ctypes.c_int()
        self.check(self.lib.cudaRuntimeGetVersion(ctypes.byref(version)), "cudaRuntimeGetVersion")
        return f"{version.value // 1000}.{version.value % 1000 // 10}"

    def gpu_arch(self):
        major, minor = ctypes.c_int(), ctypes.c_int()
        self.check(self.lib.cudaDeviceGetAttribute(ctypes.byref(major), self.ATTRIBUTE_CAPABILITY_MAJOR, 0),
                   "cudaDeviceGetAttribute")
        self.check(self.lib.cudaDeviceGetAttribute(ctypes.byref(minor), self.ATTRIBUTE_CAPABILITY_MINOR, 0),
                   "cudaDeviceGetAttribute")
        return f"sm_{major.value}{minor.value}"

    def malloc(self, size):
        address = ctypes.c_void_p()
        self.check(self.lib.cudaMalloc(ctypes.byref(address), size), "cudaMalloc")
        return address

    def free(self, address):
        self.lib.cudaFree(address)

    def stream(self):
        """A stream at the lowest priority, so apps sharing the GPU go first."""
        least, greatest = ctypes.c_int(), ctypes.c_int()
        self.check(self.lib.cudaDeviceGetStreamPriorityRange(ctypes.byref(least), ctypes.byref(greatest)),
                   "cudaDeviceGetStreamPriorityRange")
        stream = ctypes.c_void_p()
        self.check(self.lib.cudaStreamCreateWithPriority(ctypes.byref(stream), self.STREAM_NON_BLOCKING, least.value),
                   "cudaStreamCreateWithPriority")
        return stream

    def to_device(self, destination, array, stream):
        self.check(self.lib.cudaMemcpyAsync(destination, array.ctypes.data, array.nbytes,
                                            self.MEMCPY_HOST_TO_DEVICE, stream), "cudaMemcpyAsync")

    def to_host(self, array, source, stream):
        self.check(self.lib.cudaMemcpyAsync(array.ctypes.data, source, array.nbytes,
                                            self.MEMCPY_DEVICE_TO_HOST, stream), "cudaMemcpyAsync")

    def synchronize(self, stream):
        self.check(self.lib.cudaStreamSynchronize(stream), "cudaStreamSynchronize")

    def destroy_stream(self, stream):
        self.lib.cudaStreamDestroy(stream)

    def reset(self):
        """Releases this process's CUDA context and everything in it."""
        self.lib.cudaDeviceReset()


class CpuEngine:
    """ONNX Runtime on the CPU: the engine every campaign could always use."""

    name = "cpu"
    precision = "fp32"

    def __init__(self, session, input_name, runtime="ONNX Runtime"):
        self.session, self.input_name, self.runtime = session, input_name, runtime

    def infer(self, tensor):
        return self.session.run(None, {self.input_name: tensor})[0]

    def close(self):
        pass


class TensorRTEngine:
    """A deserialized plan with one float32 input and one float32 output, run
    on one low-priority stream."""

    name = "tensorrt"
    precision = PRECISION

    def __init__(self, trt, cuda, plan, input_shape, output_shape):
        import numpy as np

        self.trt, self.cuda, self.np = trt, cuda, np
        self.runtime = f"TensorRT {trt.__version__}"
        self.buffers, self.stream = [], None
        logger = trt.Logger(trt.Logger.WARNING)
        self.trt_runtime = trt.Runtime(logger)
        self.engine = self.trt_runtime.deserialize_cuda_engine(plan)
        if self.engine is None:
            raise EngineError("TensorRT could not deserialize the engine plan")
        try:
            self.context = self.engine.create_execution_context()
            if self.context is None:
                raise EngineError("TensorRT could not create an execution context")
            names = [self.engine.get_tensor_name(i) for i in range(self.engine.num_io_tensors)]
            inputs = [n for n in names if self.engine.get_tensor_mode(n) == trt.TensorIOMode.INPUT]
            outputs = [n for n in names if self.engine.get_tensor_mode(n) == trt.TensorIOMode.OUTPUT]
            if len(inputs) != 1 or len(outputs) != 1:
                raise EngineError("the engine must have one input and one output")
            for name, shape in ((inputs[0], input_shape), (outputs[0], output_shape)):
                if list(self.engine.get_tensor_shape(name)) != list(shape):
                    raise EngineError(f"engine tensor {name} has shape {list(self.engine.get_tensor_shape(name))}, "
                                      f"the model has {list(shape)}")
                if self.engine.get_tensor_dtype(name) != trt.float32:
                    raise EngineError(f"engine tensor {name} is not float32")
            self.output = np.empty(output_shape, dtype=np.float32)
            self.stream = cuda.stream()
            self.input_buffer = cuda.malloc(int(np.prod(input_shape)) * 4)
            self.buffers.append(self.input_buffer)
            self.output_buffer = cuda.malloc(self.output.nbytes)
            self.buffers.append(self.output_buffer)
            self.context.set_tensor_address(inputs[0], self.input_buffer.value)
            self.context.set_tensor_address(outputs[0], self.output_buffer.value)
        except BaseException:
            self.close()
            raise

    def device_bytes(self):
        """Activation memory TensorRT asked for, a lower bound on what the
        engine takes."""
        size = getattr(self.engine, "device_memory_size_v2", None)
        if size is None:
            size = getattr(self.engine, "device_memory_size", 0)
        return int(size or 0)

    def infer(self, tensor):
        tensor = self.np.ascontiguousarray(tensor, dtype=self.np.float32)
        self.cuda.to_device(self.input_buffer, tensor, self.stream)
        if not self.context.execute_async_v3(self.stream.value or 0):
            raise EngineError("TensorRT execute_async_v3 failed")
        self.cuda.to_host(self.output, self.output_buffer, self.stream)
        self.cuda.synchronize(self.stream)
        return self.output.copy()

    def close(self):
        # TensorRT objects go before the memory and stream they point at.
        for name in ("context", "engine", "trt_runtime"):
            if hasattr(self, name):
                delattr(self, name)
        for buffer in self.buffers:
            self.cuda.free(buffer)
        self.buffers = []
        if self.stream is not None:
            self.cuda.destroy_stream(self.stream)
            self.stream = None


# ---------------------------------------------------------------------------
# Parity


def iou(a, b):
    width = max(0.0, min(a[2], b[2]) - max(a[0], b[0]))
    height = max(0.0, min(a[3], b[3]) - max(a[1], b[1]))
    intersection = width * height
    union = (a[2] - a[0]) * (a[3] - a[1]) + (b[2] - b[0]) * (b[3] - b[1]) - intersection
    return intersection / union if union > 0 else 0.0


def _unmatched(ours, theirs, threshold):
    """The first detection in ours, clearly above threshold, that theirs lacks,
    and the smallest IoU among the ones it has."""
    worst = 1.0
    for detection in ours:
        if detection["score"] < threshold + PARITY_SCORE:
            continue
        same = [d for d in theirs if d["label"] == detection["label"]]
        best = max(same, key=lambda d: iou(detection["box"], d["box"]), default=None)
        overlap = 0.0 if best is None else iou(detection["box"], best["box"])
        if best is None or overlap < PARITY_IOU or abs(best["score"] - detection["score"]) > PARITY_SCORE:
            return detection, overlap
        worst = min(worst, overlap)
    return None, worst


def parity(cpu, gpu, tensors, decode, threshold):
    """Scores the same tensors on both engines. The result's problem is None
    when TensorRT agrees with the CPU within the stated tolerances."""
    import numpy as np

    result = {"passed": False, "frames": 0, "max_score_delta": 0.0, "min_iou": 1.0}
    for tensor in tensors:
        expected, actual = cpu.infer(tensor), gpu.infer(tensor)
        result["frames"] += 1
        if expected.shape != actual.shape or not np.isfinite(actual).all():
            return result, "tensorrt output has the wrong shape or non-finite values"
        delta = float(np.abs(expected[0, 4:, :] - actual[0, 4:, :]).max()) if expected.shape[1] > 4 else 0.0
        result["max_score_delta"] = max(result["max_score_delta"], round(delta, 4))
        cpu_detections, gpu_detections = decode(expected), decode(actual)
        for ours, theirs, side in ((cpu_detections, gpu_detections, "the cpu"),
                                   (gpu_detections, cpu_detections, "tensorrt")):
            missing, worst = _unmatched(ours, theirs, threshold)
            result["min_iou"] = round(min(result["min_iou"], worst), 4)
            if missing is not None:
                return result, (f"tensorrt disagrees with the cpu: {side} found {missing['label']} "
                                f"{missing['score']:.2f} that the other has no match for "
                                f"(IoU >= {PARITY_IOU}, score within {PARITY_SCORE})")
        if delta > PARITY_RAW_SCORE:
            return result, f"tensorrt disagrees with the cpu: a class score differs by {delta:.3f} (> {PARITY_RAW_SCORE})"
    result["passed"] = True
    return result, None


# ---------------------------------------------------------------------------
# The accelerator in the worker


def plan_status(plan, runtime, precision="fp32"):
    """Status of a worker that only ever runs on the CPU, as the agent planned it."""
    plan = plan or {}
    return {"requested": plan.get("requested", "auto"), "active": "cpu",
            "candidates": plan.get("candidates") or ["cpu"], "degraded": bool(plan.get("degraded")),
            "reason": plan.get("reason", ""), "runtime": runtime, "precision": precision}


class Accelerator:
    """Chooses and runs the engine for one YOLO model.

    infer() is called from the worker's scoring loop. A background thread
    finds or builds the TensorRT engine, checks it against the CPU and swaps
    it in. The CPU engine stays loaded throughout, so a GPU failure costs one
    frame's latency, never a frame."""

    def __init__(self, plan, cpu, model, emit, exit=os._exit):
        """plan is the agent's accelerator_plan. model holds path, sha256,
        input_shape, output_shape, threshold and decode(output) for parity."""
        import numpy as np

        self.np = np
        self.plan = plan or {}
        self.tensorrt_plan = self.plan.get("tensorrt") or {}
        self.strict = self.plan.get("requested") == "tensorrt"
        self.cpu, self.model, self.emit, self.exit = cpu, model, emit, exit
        self.lock = threading.Lock()
        self.stopped = threading.Event()
        # The engine scoring frames. A tensorrt campaign has none until its
        # engine is ready: it never scores on the CPU.
        self.engine = None if self.strict else cpu
        self.gpu = None
        self.cuda = None
        self.offered = "tensorrt" in (self.plan.get("candidates") or []) and bool(self.tensorrt_plan)
        # Recent preprocessed frames, kept for the parity check until it ran.
        self.frames = deque(maxlen=PARITY_FRAMES)
        self.collecting = self.offered
        self.latencies = deque(maxlen=256)
        self.scored = deque(maxlen=512)
        self.count = 0
        self.state = plan_status(self.plan, cpu.runtime, cpu.precision)
        self.state["model_sha256"] = model["sha256"]
        if self.strict:
            self.state.update(active="", runtime="", precision="")
        if self.offered:
            self.state["reason"] = "the TensorRT engine is not ready yet"

    # -- the scoring loop ---------------------------------------------------

    def infer(self, tensor):
        """The model output and the engine that produced it, or (None, None)
        while a tensorrt campaign waits for its engine."""
        if self.collecting:
            self.frames.append(tensor)
        engine = self.engine
        if engine is None:
            return None, None
        started = time.perf_counter()
        try:
            output = engine.infer(tensor)
        except Exception as exc:
            if engine is self.cpu or self.strict:
                raise
            self.fall_back(f"tensorrt inference failed ({type(exc).__name__}: {short(exc, 200)})")
            engine = self.cpu
            started = time.perf_counter()
            output = engine.infer(tensor)
        finished = time.perf_counter()
        with self.lock:
            if engine is self.engine:
                self.latencies.append((finished - started) * 1000)
                self.scored.append(time.monotonic())
                self.count += 1
        return output, engine

    # -- status -------------------------------------------------------------

    def status(self):
        with self.lock:
            state = json.loads(json.dumps(self.state))
        engine = state.get("engine")
        if engine:
            started = engine.pop("started", None)
            if engine.get("state") == "building" and started is not None:
                engine["elapsed_seconds"] = round(time.monotonic() - started, 1)
        return state

    def stats(self):
        with self.lock:
            latencies = sorted(self.latencies)
            scored = list(self.scored)
            count = self.count
        now = time.monotonic()
        recent = [t for t in scored if now - t <= 30]
        span = recent[-1] - recent[0] if len(recent) > 1 else 0

        def percentile(fraction):
            if not latencies:
                return 0.0
            return round(latencies[min(len(latencies) - 1, int(fraction * len(latencies)))], 2)
        return {"fps": round((len(recent) - 1) / span, 2) if span > 0 else 0.0,
                "infer_ms_p50": percentile(0.5), "infer_ms_p95": percentile(0.95), "frames_scored": count}

    def report(self):
        self.emit({"type": "accelerator", "accelerator": self.status(), "stats": self.stats()})

    def update(self, engine=None, **changes):
        """Changes the status and reports it at once."""
        with self.lock:
            self.state.update(changes)
            if engine is not None:
                current = self.state.setdefault("engine", {})
                if engine.get("state", current.get("state")) != current.get("state"):
                    current.pop("error", None)
                current.update(engine)
        self.report()

    def fall_back(self, reason):
        """Stops using TensorRT. On auto the CPU carries on, reported as
        degraded; on tensorrt the worker ends with reason as its error."""
        print(f"accelerator: {reason}", file=sys.stderr, flush=True)
        with self.lock:
            gpu, self.gpu = self.gpu, None
            self.engine = None if self.strict else self.cpu
            self.collecting = False
            self.frames.clear()
            self.latencies.clear()
            engine = self.state.get("engine")
            if engine is not None and engine.get("state") != "failed":
                engine.update(state="failed", error=reason)
        if gpu is not None:
            gpu.close()
        if self.cuda is not None:
            self.cuda.reset()
            self.cuda = None
        if self.strict:
            self.update(active="", degraded=True, reason=reason)
            self.emit({"type": "error", "error": short(reason, 500)})
            self.exit(1)
            return
        self.update(active="cpu", degraded=True, reason=reason, runtime=self.cpu.runtime,
                    precision=self.cpu.precision, gpu_memory_mib=0)

    # -- the background thread ----------------------------------------------

    def start(self):
        threading.Thread(target=self._report_periodically, daemon=True).start()
        if self.offered:
            threading.Thread(target=self._accelerate_safely, daemon=True).start()
        elif self.strict:
            self.fall_back("tensorrt is unavailable: " + (self.plan.get("reason") or "the agent offered no TensorRT"))

    def stop(self):
        self.stopped.set()

    def wait(self, seconds):
        """False when the worker is stopping."""
        return not self.stopped.wait(seconds)

    def _report_periodically(self):
        while self.wait(STATUS_SECONDS):
            self.report()

    def _accelerate_safely(self):
        try:
            self.accelerate()
        except Exception as exc:
            self.fall_back(f"tensorrt: {type(exc).__name__}: {short(exc)}")

    # The steps that touch TensorRT, CUDA or the machine are methods, so tests
    # can replace them.

    def import_tensorrt(self):
        bindings = self.tensorrt_plan["bindings"]
        # Appended, never prepended: nothing in the bindings directory can
        # shadow a package of the worker's own locked environment.
        if bindings not in sys.path:
            sys.path.append(bindings)
        import tensorrt
        return tensorrt

    def open_cuda(self):
        return Cuda(self.tensorrt_plan["cuda_runtime"])

    def available_mib(self):
        return available_mib()

    def load_engine(self, trt, cuda, plan):
        return TensorRTEngine(trt, cuda, plan, self.model["input_shape"], self.model["output_shape"])

    def parity_tensors(self):
        noise = self.np.random.default_rng(0).random(tuple(self.model["input_shape"]), dtype=self.np.float32)
        return list(self.frames) + [noise]

    def accelerate(self):
        try:
            trt = self.import_tensorrt()
        except Exception as exc:
            return self.fall_back(f"the TensorRT Python bindings did not load ({type(exc).__name__}: {short(exc, 200)})")
        try:
            cuda = self.open_cuda()
            cuda_version, gpu_arch = cuda.runtime_version(), cuda.gpu_arch()
            # Do not hold a CUDA context through a build that may take minutes;
            # loading the engine creates a fresh one.
            cuda.reset()
        except Exception as exc:
            return self.fall_back(f"CUDA is unavailable ({short(exc, 200)})")
        plan = self.tensorrt_plan
        fields = key_fields(self.model["sha256"], trt.__version__, plan.get("library", ""), cuda_version,
                            gpu_arch, self.model["input_shape"], self.model["output_shape"], plan["workspace_mib"])
        key = cache_key(fields)
        cache = EngineCache(plan["engine_dir"])
        os.makedirs(cache.root, mode=0o700, exist_ok=True)
        self.update(gpu_arch=gpu_arch, engine={"state": "checking", "key": key[:16]})
        built = rebuilt = False
        while not self.stopped.is_set():
            engine_plan, record, problem = cache.lookup(key, fields)
            if engine_plan is None:
                if problem:
                    print(f"accelerator: rebuilding the cached engine: {problem}", file=sys.stderr, flush=True)
                failure = cache.failed(key)
                if failure:
                    return self.fall_back(failure)
                outcome, detail = self.build(key, fields)
            elif not self.admit_load():
                continue
            else:
                outcome = self.load(trt, engine_plan, record, cached=not built)
                if outcome != "retry":
                    return
                # The plan matched its record and still did not deserialize.
                # Rebuild it once; a second failure is final.
                if rebuilt:
                    cache.tombstone(key, "a freshly built engine did not deserialize")
                    return self.fall_back("the TensorRT engine did not deserialize, even after a rebuild")
                rebuilt = True
                outcome, detail = self.build(key, fields, discard=record["plan_sha256"])
            if outcome == "stopped":
                return
            if outcome == "failed":
                cache.tombstone(key, detail)
                return self.fall_back(f"the TensorRT engine build failed: {detail}")
            built = built or outcome == "done"

    def admit_load(self):
        budget = self.tensorrt_plan["gpu_budget_mib"]
        available = self.available_mib()
        if available is None or available >= budget:
            return True
        self.update(engine={"state": "waiting_for_memory",
                            "error": f"{available} MiB of memory is available; loading the engine needs {budget} MiB"})
        self.wait(MEMORY_RETRY_SECONDS)
        return False

    def load(self, trt, engine_plan, record, cached):
        """Loads the engine, holds it to its memory budget and the CPU's
        detections, and swaps it in. Returns ready, failed, stopped, or retry
        when the plan would not deserialize."""
        budget = self.tensorrt_plan["gpu_budget_mib"]
        self.update(engine={"state": "verifying", "cached": cached, "plan_bytes": record["plan_bytes"],
                            "build_seconds": record.get("build_seconds", 0)})
        # Measured across the CUDA context and the engine together: on a
        # Jetson the GPU allocates from the memory MemAvailable counts.
        before = self.available_mib()
        try:
            self.cuda = self.open_cuda()
            gpu = self.load_engine(trt, self.cuda, engine_plan)
        except EngineError as exc:
            if "deserialize" in str(exc):
                self.cuda.reset()
                self.cuda = None
                return "retry"
            self.fall_back(f"the TensorRT engine did not load ({short(exc, 200)})")
            return "failed"
        after = self.available_mib()
        measured = max(0, before - after) if before is not None and after is not None else 0
        used = max(measured, (gpu.device_bytes() + record["plan_bytes"]) / (1 << 20))
        with self.lock:
            self.gpu = gpu
        if used > budget:
            self.fall_back(f"the TensorRT engine took {used:.0f} MiB, over its {budget} MiB budget")
            return "failed"
        self.update(gpu_memory_mib=round(used, 1))
        if not self.frames:
            # Nothing to compare yet, and nothing to score either.
            self.update(engine={"error": "waiting for a camera frame to compare the cpu and tensorrt on"})
            while not self.frames:
                if not self.wait(1):
                    return "stopped"
        result, problem = parity(self.cpu, gpu, self.parity_tensors(), self.model["decode"], self.model["threshold"])
        with self.lock:
            self.state["parity"] = result
        if problem:
            self.fall_back(problem)
            return "failed"
        with self.lock:
            self.engine = gpu
            self.collecting = False
            self.frames.clear()
            self.latencies.clear()
        self.update(active="tensorrt", degraded=False, reason="", runtime=gpu.runtime,
                    precision=gpu.precision, engine={"state": "ready"})
        print(f"accelerator: scoring on {gpu.runtime} {gpu.precision}; parity over {result['frames']} frames, "
              f"max score delta {result['max_score_delta']}", file=sys.stderr, flush=True)
        return "ready"

    def build(self, key, fields, discard=None):
        """Builds the engine in a child process. (outcome, detail): outcome is
        done, failed, waiting_for_memory or stopped."""
        plan = self.tensorrt_plan
        minimum = plan["build_min_available_mib"]
        available = self.available_mib()
        if available is not None and available < minimum:
            return self.wait_for_memory(available, minimum)
        job = {"key": key, "fields": fields, "model_path": self.model["path"], "bindings": plan["bindings"],
               "engine_dir": plan["engine_dir"], "workspace_mib": plan["workspace_mib"],
               "build_min_available_mib": minimum, "discard_plan_sha256": discard}
        self.update(engine={"state": "queued"})
        return self.run_builder(job, plan.get("build_timeout_seconds", 1800))

    def wait_for_memory(self, available, minimum):
        self.update(engine={"state": "waiting_for_memory",
                            "error": f"{available} MiB of memory is available; a build needs {minimum} MiB"})
        return ("waiting_for_memory", "") if self.wait(MEMORY_RETRY_SECONDS) else ("stopped", "")

    def run_builder(self, job, timeout):
        """Runs accelerator.py --build-engine. The child takes the device-wide
        build lock itself, so the kernel releases it however the child ends,
        and no crash or restart can keep it. The child stays in the worker's
        process group, so stopping the worker stops the build too."""
        child = subprocess.Popen([sys.executable, "-u", os.path.abspath(__file__), "--build-engine"],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE, close_fds=True)
        last = {}

        def read():
            for line in child.stdout:
                try:
                    message = json.loads(line)
                except ValueError:
                    continue
                last.clear()
                last.update(message)
                if message.get("state") == "building":
                    self.update(engine={"state": "building", "started_at": message.get("started_at") or utc_now(),
                                        "started": time.monotonic()})
                elif message.get("state") == "queued":
                    self.update(engine={"state": "queued", "error": "another campaign's engine is building"})

        reader = threading.Thread(target=read, daemon=True)
        reader.start()
        try:
            child.stdin.write(json.dumps(job).encode())
            child.stdin.close()
        except OSError:
            pass
        deadline = time.monotonic() + timeout
        while child.poll() is None:
            if self.stopped.is_set() or time.monotonic() > deadline:
                child.kill()
                child.wait()
                if self.stopped.is_set():
                    return "stopped", ""
                return "failed", f"the build took longer than {timeout // 60} minutes"
            time.sleep(0.5)
        reader.join(timeout=5)
        child.stdout.close()
        if child.returncode == 0:
            return "done", ""
        if child.returncode == EXIT_WAITING_FOR_MEMORY:
            return self.wait_for_memory(last.get("available_mib"), job["build_min_available_mib"])
        if last.get("error"):
            return "failed", last["error"]
        if child.returncode < 0:
            return "failed", f"the build process was killed by signal {-child.returncode}, possibly for memory"
        return "failed", f"the build process exited with code {child.returncode}"


# ---------------------------------------------------------------------------
# The --build-engine child


def _exit_with(parent):
    """Ends the builder when the worker that started it is gone."""
    while True:
        time.sleep(5)
        if os.getppid() != parent:
            os._exit(1)


def build_plan(trt, model, workspace_mib):
    logger = trt.Logger(trt.Logger.WARNING)
    builder = trt.Builder(logger)
    network = builder.create_network(0)
    parser = trt.OnnxParser(network, logger)
    if not parser.parse(model):
        errors = "; ".join(str(parser.get_error(i)) for i in range(parser.num_errors))
        raise EngineError("TensorRT could not parse the ONNX model: " + short(errors, 300))
    config = builder.create_builder_config()
    config.set_memory_pool_limit(trt.MemoryPoolType.WORKSPACE, workspace_mib << 20)
    config.set_flag(trt.BuilderFlag.FP16)
    config.builder_optimization_level = OPTIMIZATION_LEVEL
    serialized = builder.build_serialized_network(network, config)
    if serialized is None:
        raise EngineError("TensorRT could not build an engine for this model")
    plan = bytes(memoryview(serialized))
    if not 0 < len(plan) <= MAX_PLAN_BYTES:
        raise EngineError("TensorRT built an empty or oversized engine")
    # Promote only a plan that deserializes.
    if trt.Runtime(logger).deserialize_cuda_engine(plan) is None:
        raise EngineError("the built engine did not deserialize")
    return plan


def build_main(stdin=None, stdout=None):
    stdin = stdin or sys.stdin.buffer
    stdout = stdout or sys.stdout

    def say(**message):
        print(json.dumps(message), file=stdout, flush=True)

    try:
        os.nice(10)
    except OSError:
        pass
    threading.Thread(target=_exit_with, args=(os.getppid(),), daemon=True).start()
    try:
        job = json.loads(stdin.read(1 << 20))
        cache = EngineCache(job["engine_dir"])
        os.makedirs(cache.root, mode=0o700, exist_ok=True)
        lock = os.open(os.path.join(cache.root, ".build.lock"), os.O_RDWR | os.O_CREAT, 0o600)
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            say(state="queued")
            fcntl.flock(lock, fcntl.LOCK_EX)
        # The lock is held until this process exits, whichever way it does.
        cache.remove_partial_builds()
        plan, record, _ = cache.lookup(job["key"], job["fields"])
        if plan is not None and record["plan_sha256"] != job.get("discard_plan_sha256"):
            # Another campaign built it while this one waited for the lock.
            say(state="done", cached=True)
            return 0
        cache.discard(job["key"])
        available = available_mib()
        if available is not None and available < job["build_min_available_mib"]:
            say(state="waiting_for_memory", available_mib=available)
            return EXIT_WAITING_FOR_MEMORY
        say(state="building", started_at=utc_now())
        started = time.monotonic()
        with open(job["model_path"], "rb") as handle:
            model = handle.read((512 << 20) + 1)
        if hashlib.sha256(model).hexdigest() != job["fields"]["model_sha256"]:
            raise EngineError("the model file changed since the worker loaded it")
        if job["bindings"] not in sys.path:
            sys.path.append(job["bindings"])
        import tensorrt
        if tensorrt.__version__ != job["fields"]["tensorrt"]:
            raise EngineError(f"TensorRT {tensorrt.__version__} loaded, the worker has {job['fields']['tensorrt']}")
        plan = build_plan(tensorrt, model, job["workspace_mib"])
        record = cache.store(job["key"], job["fields"], plan, time.monotonic() - started)
        say(state="done", build_seconds=record["build_seconds"], plan_bytes=record["plan_bytes"])
        return 0
    except Exception as exc:
        say(state="failed", error=short(f"{type(exc).__name__}: {exc}", 400))
        return 1


if __name__ == "__main__":
    if sys.argv[1:] == ["--build-engine"]:
        sys.exit(build_main())
    sys.exit("usage: accelerator.py --build-engine < job.json")
