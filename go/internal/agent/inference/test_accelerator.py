"""Engine selection, the engine cache, the builder and the parity check, with
TensorRT and CUDA replaced by fakes: no GPU is needed."""

import fcntl
import json
import os
import pathlib
import signal
import subprocess
import sys
import tempfile
import textwrap
import time
import types
import unittest
from unittest import mock

import accelerator

try:
    import numpy as np
except ImportError:  # pragma: no cover
    np = None

PLAN_SHA = "5" * 64
LABELS = ["person", "dog"]


def fields(**changes):
    values = dict(model_sha256="a" * 64, tensorrt_version="10.16.2.10", library_version="10.16.2",
                  cuda_version="13.2", gpu_arch="sm_87", input_shape=[1, 3, 640, 640],
                  output_shape=[1, 6, 8400], workspace_mib=1024)
    values.update(changes)
    return accelerator.key_fields(**values)


class CacheTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.cache = accelerator.EngineCache(directory.name)

    def test_every_field_is_in_the_key(self):
        base = accelerator.cache_key(fields())
        for change in (dict(model_sha256="b" * 64), dict(tensorrt_version="10.16.3.1"),
                       dict(library_version="10.16.3"), dict(cuda_version="13.3"), dict(gpu_arch="sm_110"),
                       dict(input_shape=[1, 3, 320, 320]), dict(output_shape=[1, 6, 2100]),
                       dict(workspace_mib=512)):
            self.assertNotEqual(accelerator.cache_key(fields(**change)), base, change)
        self.assertEqual(accelerator.cache_key(fields()), base)
        self.assertEqual(fields()["precision"], "fp16")

    def test_stored_engine_verifies(self):
        key = accelerator.cache_key(fields())
        self.assertEqual(self.cache.lookup(key, fields()), (None, None, None))
        record = self.cache.store(key, fields(), b"plan bytes", 12.5)
        plan, found, problem = self.cache.lookup(key, fields())
        self.assertEqual((plan, problem), (b"plan bytes", None))
        self.assertEqual(found["plan_bytes"], 10)
        self.assertEqual(found, record)
        self.assertEqual(os.listdir(self.cache.root), [key[:32]])

    def test_tampered_plan_is_refused(self):
        key = accelerator.cache_key(fields())
        self.cache.store(key, fields(), b"plan bytes", 1)
        path = pathlib.Path(self.cache.directory(key)) / "engine.plan"
        path.write_bytes(b"plan bytez")
        plan, _, problem = self.cache.lookup(key, fields())
        self.assertIsNone(plan)
        self.assertEqual(problem, "engine plan does not match its record")

    def test_record_for_another_tensorrt_is_refused(self):
        # An OS update that changes TensorRT changes the key, so the old
        # engine is never found. A record edited to claim the new version
        # still names the old fields and is refused.
        key = accelerator.cache_key(fields())
        self.cache.store(key, fields(), b"plan bytes", 1)
        newer = fields(tensorrt_version="10.17.0.1")
        self.assertEqual(self.cache.lookup(accelerator.cache_key(newer), newer), (None, None, None))
        path = pathlib.Path(self.cache.directory(key)) / "engine.json"
        record = json.loads(path.read_text())
        record["fields"]["tensorrt"] = "10.17.0.1"
        path.write_text(json.dumps(record))
        plan, _, problem = self.cache.lookup(key, fields())
        self.assertIsNone(plan)
        self.assertIn("does not match", problem)

    def test_partial_build_is_invisible_and_removed(self):
        key = accelerator.cache_key(fields())
        partial = pathlib.Path(self.cache.root) / ".tmp-crashed"
        partial.mkdir()
        (partial / "engine.plan").write_bytes(b"half a plan")
        self.assertEqual(self.cache.lookup(key, fields()), (None, None, None))
        self.cache.remove_partial_builds()
        self.assertFalse(partial.exists())

    def test_tombstone_holds_for_a_day(self):
        key = accelerator.cache_key(fields())
        now = 1_000_000.0
        self.assertIsNone(self.cache.failed(key, now))
        self.cache.tombstone(key, "parser said no", now)
        self.assertIn("parser said no", self.cache.failed(key, now + 3600))
        self.assertIsNone(self.cache.failed(key, now + accelerator.TOMBSTONE_SECONDS + 1))
        self.assertIsNone(self.cache.failed(accelerator.cache_key(fields(model_sha256="c" * 64)), now))


FAKE_TENSORRT = '''
import os, time
__version__ = "10.16.2.10"
class Logger:
    WARNING = 2
    def __init__(self, severity=None): pass
class MemoryPoolType: WORKSPACE = 0
class BuilderFlag: FP16 = 1
class OnnxParser:
    num_errors = 1
    def __init__(self, network, logger): pass
    def parse(self, model): return model.startswith(b"onnx")
    def get_error(self, i): return "not an onnx model"
class _Config:
    builder_optimization_level = 0
    def set_memory_pool_limit(self, pool, size): self.workspace = size
    def set_flag(self, flag): self.flag = flag
class Builder:
    def __init__(self, logger): pass
    def create_network(self, flags): return object()
    def create_builder_config(self): return _Config()
    def build_serialized_network(self, network, config):
        behaviour = os.environ.get("FAKE_TENSORRT_BUILD", "ok")
        if behaviour == "hang":
            print("hanging", flush=True)
            time.sleep(120)
        if behaviour == "none":
            return None
        return b"plan:%d:%d" % (config.workspace >> 20, config.flag)
class Runtime:
    def __init__(self, logger): pass
    def deserialize_cuda_engine(self, plan):
        return object() if plan.startswith(b"plan:") else None
'''


class BuilderTests(unittest.TestCase):
    """The --build-engine child, as a real process with a fake TensorRT."""

    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        root = pathlib.Path(directory.name)
        (root / "bindings" / "tensorrt").mkdir(parents=True)
        (root / "bindings" / "tensorrt" / "__init__.py").write_text(FAKE_TENSORRT)
        self.model = root / "model.onnx"
        self.model.write_bytes(b"onnx model bytes")
        import hashlib
        self.fields = fields(model_sha256=hashlib.sha256(self.model.read_bytes()).hexdigest())
        self.key = accelerator.cache_key(self.fields)
        self.engines = root / "engines"
        self.job = {"key": self.key, "fields": self.fields, "model_path": str(self.model),
                    "bindings": str(root / "bindings"), "engine_dir": str(self.engines),
                    "workspace_mib": 1024, "build_min_available_mib": 0}

    def builder(self, behaviour="ok", job=None):
        env = dict(os.environ, FAKE_TENSORRT_BUILD=behaviour)
        child = subprocess.Popen([sys.executable, "-u", accelerator.__file__, "--build-engine"],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE, env=env)
        child.stdin.write(json.dumps(job or self.job).encode())
        child.stdin.close()
        return child

    def finish(self, child):
        output = child.stdout.read()
        child.stdout.close()
        child.wait(timeout=30)
        return child.returncode, [json.loads(line) for line in output.splitlines() if line.startswith(b"{")]

    def test_build_promotes_a_verified_engine(self):
        code, messages = self.finish(self.builder())
        self.assertEqual(code, 0, messages)
        self.assertEqual([m["state"] for m in messages], ["building", "done"])
        plan, record, problem = accelerator.EngineCache(str(self.engines)).lookup(self.key, self.fields)
        self.assertIsNone(problem)
        # FP16, with the workspace limit the agent set.
        self.assertEqual(plan, b"plan:1024:1")
        self.assertEqual(record["plan_bytes"], len(plan))
        self.assertEqual(sorted(os.listdir(self.engines)), [".build.lock", self.key[:32]])

    def test_failed_build_promotes_nothing(self):
        code, messages = self.finish(self.builder("none"))
        self.assertEqual(code, 1)
        self.assertEqual(messages[-1]["state"], "failed")
        self.assertIn("could not build", messages[-1]["error"])
        self.assertEqual(os.listdir(self.engines), [".build.lock"])

    def test_model_that_changed_is_refused(self):
        self.model.write_bytes(b"onnx another model")
        code, messages = self.finish(self.builder())
        self.assertEqual(code, 1)
        self.assertIn("model file changed", messages[-1]["error"])

    def test_unparseable_model_names_the_parser_error(self):
        import hashlib
        self.model.write_bytes(b"not onnx")
        job = dict(self.job, fields=dict(self.fields, model_sha256=hashlib.sha256(b"not onnx").hexdigest()))
        code, messages = self.finish(self.builder(job=job))
        self.assertEqual(code, 1)
        self.assertIn("not an onnx model", messages[-1]["error"])

    def test_waits_for_the_lock_and_reuses_an_engine_built_meanwhile(self):
        self.engines.mkdir()
        lock = os.open(self.engines / ".build.lock", os.O_RDWR | os.O_CREAT, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX)
        child = self.builder()
        self.assertEqual(json.loads(child.stdout.readline())["state"], "queued")
        accelerator.EngineCache(str(self.engines)).store(self.key, self.fields, b"plan:built elsewhere", 1)
        os.close(lock)
        code, messages = self.finish(child)
        self.assertEqual(code, 0)
        self.assertEqual(messages, [{"state": "done", "cached": True}])

    def test_a_killed_builder_releases_the_lock(self):
        # The lock belongs to the builder process, so the kernel frees it with
        # the process: no crash, restart or backoff can keep the build slot.
        child = self.builder("hang")
        self.assertEqual(json.loads(child.stdout.readline())["state"], "building")
        self.assertEqual(child.stdout.readline().strip(), b"hanging")
        lock = os.open(self.engines / ".build.lock", os.O_RDWR)
        self.addCleanup(os.close, lock)
        with self.assertRaises(BlockingIOError):
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        child.send_signal(signal.SIGKILL)
        child.wait(timeout=10)
        child.stdout.close()
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.assertEqual(os.listdir(self.engines), [".build.lock"])

    def test_discard_replaces_an_engine_that_did_not_deserialize(self):
        cache = accelerator.EngineCache(str(self.engines))
        self.engines.mkdir()
        record = cache.store(self.key, self.fields, b"plan:stale", 1)
        code, _ = self.finish(self.builder(job=dict(self.job, discard_plan_sha256=record["plan_sha256"])))
        self.assertEqual(code, 0)
        self.assertEqual(cache.lookup(self.key, self.fields)[0], b"plan:1024:1")

    def test_build_waits_for_memory(self):
        if accelerator.available_mib() is None:
            self.skipTest("needs /proc/meminfo")
        code, messages = self.finish(self.builder(job=dict(self.job, build_min_available_mib=1 << 40)))
        self.assertEqual(code, accelerator.EXIT_WAITING_FOR_MEMORY)
        self.assertEqual(messages[-1]["state"], "waiting_for_memory")


def yolo_output(*rows, anchors=8):
    """A raw [1, 4 + classes, anchors] output; rows are cx, cy, w, h, scores."""
    output = np.zeros((1, 4 + len(LABELS), anchors), dtype=np.float32)
    for anchor, row in enumerate(rows):
        output[0, :, anchor] = row
    return output


def decode(output, threshold=0.5):
    from worker import yolo_detections
    return yolo_detections(output, LABELS, set(LABELS), threshold, 1, 0, 0, 640, 640)


class FakeEngine:
    def __init__(self, name, output, precision="fp32", runtime="fake", fail=False, device_bytes=0):
        self.name, self.output, self.precision, self.runtime = name, output, precision, runtime
        self.fail, self.calls, self.closed, self._device_bytes = fail, 0, False, device_bytes

    def infer(self, tensor):
        self.calls += 1
        if self.fail:
            raise RuntimeError("the GPU fell over")
        return self.output.copy()

    def device_bytes(self):
        return self._device_bytes

    def close(self):
        self.closed = True


@unittest.skipIf(np is None, "requires numpy")
class ParityTests(unittest.TestCase):
    PERSON = [100, 100, 50, 80, 0.9, 0.0]

    def check(self, gpu_output, cpu_output=None):
        cpu = FakeEngine("cpu", yolo_output(self.PERSON) if cpu_output is None else cpu_output)
        gpu = FakeEngine("tensorrt", gpu_output)
        return accelerator.parity(cpu, gpu, [np.zeros((1, 3, 640, 640), np.float32)] * 2, decode, 0.5)

    def test_matching_detections_pass(self):
        result, problem = self.check(yolo_output([101, 100, 50, 80, 0.88, 0.0]))
        self.assertIsNone(problem)
        self.assertTrue(result["passed"])
        self.assertEqual(result["frames"], 2)
        self.assertAlmostEqual(result["max_score_delta"], 0.02, places=3)
        self.assertGreater(result["min_iou"], 0.9)

    def test_score_drift_fails(self):
        result, problem = self.check(yolo_output([100, 100, 50, 80, 0.8, 0.0]))
        self.assertFalse(result["passed"])
        self.assertIn("the cpu found person 0.90", problem)

    def test_moved_box_fails(self):
        _, problem = self.check(yolo_output([110, 100, 50, 80, 0.9, 0.0]))
        self.assertIn("IoU >= 0.8", problem)

    def test_small_box_rounded_by_fp16_passes(self):
        # A 12 px person whose edges moved by half a pixel: IoU 0.85.
        cpu = yolo_output([100, 100, 12, 12, 0.9, 0.0])
        result, problem = self.check(yolo_output([100.5, 100.5, 12, 12, 0.9, 0.0]), cpu)
        self.assertIsNone(problem)
        self.assertLess(result["min_iou"], 0.9)

    def test_extra_tensorrt_detection_fails(self):
        _, problem = self.check(yolo_output(self.PERSON, [400, 400, 40, 40, 0.0, 0.8]))
        self.assertIn("tensorrt found dog 0.80", problem)

    def test_detection_near_the_threshold_may_flip(self):
        # 0.52 on the CPU and 0.49 on TensorRT: FP16 moved it across 0.5.
        cpu = yolo_output(self.PERSON, [400, 400, 40, 40, 0.0, 0.52])
        result, problem = self.check(yolo_output(self.PERSON, [400, 400, 40, 40, 0.0, 0.49]), cpu)
        self.assertIsNone(problem)
        self.assertTrue(result["passed"])

    def test_raw_scores_must_agree_too(self):
        gpu = yolo_output(self.PERSON)
        gpu[0, 5, 7] = 0.2  # A dog score no detection shows.
        _, problem = self.check(gpu)
        self.assertIn("class score differs by 0.200", problem)
        gpu[0, 5, 7] = 0.08  # Within the raw tolerance.
        self.assertIsNone(self.check(gpu)[1])

    def test_wrong_shape_fails(self):
        _, problem = self.check(np.zeros((1, 6, 4), np.float32))
        self.assertIn("wrong shape", problem)


class FakeCuda:
    def __init__(self):
        self.resets = 0

    def runtime_version(self):
        return "13.2"

    def gpu_arch(self):
        return "sm_87"

    def reset(self):
        self.resets += 1


class ScriptedAccelerator(accelerator.Accelerator):
    """The TensorRT, CUDA, memory and builder steps, scripted."""

    def __init__(self, plan, cpu, model, gpu=None, memory=(8000,), build="done", import_error=None):
        self.emitted, self.exits = [], []
        super().__init__(plan, cpu, model, self.emitted.append, self.exits.append)
        self.gpu_engine, self.memory, self.build_outcome = gpu, list(memory), build
        self.import_error, self.jobs, self.loads, self.fake_cuda = import_error, [], [], FakeCuda()

    def import_tensorrt(self):
        if self.import_error:
            raise ImportError(self.import_error)
        return types.SimpleNamespace(__version__="10.16.2.10")

    def open_cuda(self):
        return self.fake_cuda

    def available_mib(self):
        return self.memory.pop(0) if len(self.memory) > 1 else self.memory[0]

    def load_engine(self, trt, cuda, plan):
        self.loads.append(plan)
        if plan == b"corrupt":
            raise accelerator.EngineError("TensorRT could not deserialize the engine plan")
        return self.gpu_engine

    def run_builder(self, job, timeout):
        self.jobs.append(job)
        if self.build_outcome == "done":
            cache = accelerator.EngineCache(job["engine_dir"])
            cache.discard(job["key"])
            cache.store(job["key"], job["fields"], b"plan", 3.0)
            return "done", ""
        return self.build_outcome, "TensorRT could not build an engine for this model"


@unittest.skipIf(np is None, "requires numpy")
class AcceleratorTests(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.engines = os.path.join(directory.name, "engines")
        self.output = yolo_output([100, 100, 50, 80, 0.9, 0.0])
        self.cpu = FakeEngine("cpu", self.output, runtime="ONNX Runtime 1.23.2")
        self.gpu = FakeEngine("tensorrt", self.output, precision="fp16", runtime="TensorRT 10.16.2.10")
        self.model = {"path": "/models/yolo11n.onnx", "sha256": "a" * 64, "input_shape": [1, 3, 64, 64],
                      "output_shape": [1, 6, 8], "threshold": 0.5, "decode": decode}
        self.tensor = np.zeros((1, 3, 64, 64), np.float32)
        patcher = mock.patch.object(accelerator, "MEMORY_RETRY_SECONDS", 0)
        patcher.start()
        self.addCleanup(patcher.stop)

    def plan(self, requested="auto", **changes):
        candidates = ["tensorrt"] if requested == "tensorrt" else ["tensorrt", "cpu"]
        plan = {"requested": requested, "candidates": candidates,
                "tensorrt": {"library": "10.16.2", "bindings": "/accel/tensorrt-10.16.2.10",
                             "engine_dir": self.engines, "cuda_runtime": "libcudart.so.13",
                             "workspace_mib": 1024, "build_min_available_mib": 3072, "gpu_budget_mib": 1024,
                             "build_timeout_seconds": 1800}}
        plan.update(changes)
        return plan

    def accelerator(self, plan=None, frames=1, **options):
        options.setdefault("gpu", self.gpu)
        subject = ScriptedAccelerator(plan or self.plan(), self.cpu, self.model, **options)
        for _ in range(frames):
            subject.infer(self.tensor)
        return subject

    def test_cpu_plan_never_touches_tensorrt(self):
        subject = self.accelerator({"requested": "auto", "candidates": ["cpu"],
                                    "reason": "no supported accelerator"}, frames=0)
        subject.start()
        subject.stop()
        output, engine = subject.infer(self.tensor)
        self.assertIs(engine, self.cpu)
        status = subject.status()
        self.assertEqual((status["active"], status["degraded"], status["reason"]),
                         ("cpu", False, "no supported accelerator"))
        self.assertEqual(subject.jobs, [])
        self.assertEqual(len(subject.frames), 0)

    def test_scores_on_the_cpu_while_building_then_switches(self):
        subject = self.accelerator()
        self.assertEqual(subject.status()["active"], "cpu")
        self.assertEqual(subject.status()["reason"], "the TensorRT engine is not ready yet")
        subject.accelerate()
        self.assertEqual(len(subject.jobs), 1)
        job = subject.jobs[0]
        self.assertEqual((job["workspace_mib"], job["build_min_available_mib"], job["model_path"]),
                         (1024, 3072, "/models/yolo11n.onnx"))
        status = subject.status()
        self.assertEqual((status["active"], status["degraded"], status["reason"]), ("tensorrt", False, ""))
        self.assertEqual((status["runtime"], status["precision"], status["gpu_arch"]),
                         ("TensorRT 10.16.2.10", "fp16", "sm_87"))
        self.assertEqual(status["engine"]["state"], "ready")
        self.assertFalse(status["engine"]["cached"])
        self.assertTrue(status["parity"]["passed"])
        # The real frame and one noise frame.
        self.assertEqual(status["parity"]["frames"], 2)
        self.assertIs(subject.infer(self.tensor)[1], self.gpu)
        states = [m["accelerator"]["engine"]["state"] for m in subject.emitted if m["accelerator"].get("engine")]
        self.assertEqual(states[0], "checking")
        self.assertIn("queued", states)
        self.assertEqual(states[-1], "ready")
        json.dumps(subject.emitted)

    def test_cached_engine_loads_without_a_build(self):
        self.accelerator().accelerate()
        subject = self.accelerator()
        subject.accelerate()
        self.assertEqual(subject.jobs, [])
        self.assertEqual(subject.loads, [b"plan"])
        self.assertTrue(subject.status()["engine"]["cached"])
        self.assertEqual(subject.status()["active"], "tensorrt")

    def test_tampered_engine_is_rebuilt(self):
        self.accelerator().accelerate()
        plan_path = next(pathlib.Path(self.engines).glob("*/engine.plan"))
        plan_path.write_bytes(b"evil")
        subject = self.accelerator()
        subject.accelerate()
        self.assertEqual(len(subject.jobs), 1)
        self.assertEqual(subject.loads, [b"plan"])
        self.assertEqual(subject.status()["active"], "tensorrt")

    def test_engine_that_does_not_deserialize_is_rebuilt_once(self):
        first = self.accelerator()
        first.accelerate()
        record = next(pathlib.Path(self.engines).glob("*/engine.json"))
        cache = accelerator.EngineCache(self.engines)
        key = json.loads(record.read_text())["key"]
        cache.store(key, json.loads(record.read_text())["fields"], b"corrupt", 1)
        subject = self.accelerator()
        subject.accelerate()
        self.assertEqual(subject.loads, [b"corrupt", b"plan"])
        import hashlib
        self.assertEqual(subject.jobs[0]["discard_plan_sha256"], hashlib.sha256(b"corrupt").hexdigest())
        self.assertEqual(subject.status()["active"], "tensorrt")

    def test_failed_build_falls_back_loudly_and_is_not_retried(self):
        subject = self.accelerator(build="failed")
        subject.accelerate()
        status = subject.status()
        self.assertEqual((status["active"], status["degraded"]), ("cpu", True))
        self.assertIn("could not build an engine", status["reason"])
        self.assertEqual(status["engine"]["state"], "failed")
        self.assertEqual(subject.fake_cuda.resets, 1)
        self.assertIs(subject.infer(self.tensor)[1], self.cpu)
        again = self.accelerator()
        again.accelerate()
        self.assertEqual(again.jobs, [])
        self.assertIn("is retried in about 24h", again.status()["reason"])
        self.assertTrue(again.status()["degraded"])

    def test_missing_bindings_fall_back_with_the_reason(self):
        subject = self.accelerator(import_error="No module named 'tensorrt'")
        subject.accelerate()
        status = subject.status()
        self.assertEqual((status["active"], status["degraded"]), ("cpu", True))
        self.assertIn("the TensorRT Python bindings did not load", status["reason"])
        self.assertEqual(subject.jobs, [])

    def test_build_waits_for_memory(self):
        # 2 GiB free: no build. Then 4 GiB: the build runs.
        subject = self.accelerator(memory=(2048, 4096, 4000, 3900))
        subject.accelerate()
        states = [m["accelerator"]["engine"]["state"] for m in subject.emitted if m["accelerator"].get("engine")]
        self.assertIn("waiting_for_memory", states)
        waiting = next(m for m in subject.emitted if m["accelerator"].get("engine", {}).get("state") == "waiting_for_memory")
        self.assertEqual(waiting["accelerator"]["engine"]["error"],
                         "2048 MiB of memory is available; a build needs 3072 MiB")
        self.assertEqual(waiting["accelerator"]["active"], "cpu")
        self.assertEqual(len(subject.jobs), 1)
        self.assertEqual(subject.status()["active"], "tensorrt")

    def test_engine_over_its_memory_budget_falls_back(self):
        subject = self.accelerator(gpu=FakeEngine("tensorrt", self.output, "fp16", device_bytes=2 << 30))
        subject.accelerate()
        status = subject.status()
        self.assertEqual((status["active"], status["degraded"]), ("cpu", True))
        self.assertIn("over its 1024 MiB budget", status["reason"])
        self.assertTrue(subject.gpu_engine.closed)

    def test_disagreeing_engine_stays_off(self):
        gpu = FakeEngine("tensorrt", yolo_output([100, 100, 50, 80, 0.6, 0.0]), "fp16")
        subject = self.accelerator(gpu=gpu)
        subject.accelerate()
        status = subject.status()
        self.assertEqual((status["active"], status["degraded"]), ("cpu", True))
        self.assertIn("tensorrt disagrees with the cpu", status["reason"])
        self.assertFalse(status["parity"]["passed"])
        self.assertTrue(gpu.closed)

    def test_gpu_failure_while_scoring_costs_no_frame(self):
        gpu = FakeEngine("tensorrt", self.output, "fp16")
        subject = self.accelerator(gpu=gpu)
        subject.accelerate()
        gpu.fail = True
        output, engine = subject.infer(self.tensor)
        self.assertIs(engine, self.cpu)
        self.assertEqual(output.shape, self.output.shape)
        status = subject.status()
        self.assertEqual((status["active"], status["degraded"]), ("cpu", True))
        self.assertIn("the GPU fell over", status["reason"])

    def test_tensorrt_campaign_never_scores_on_the_cpu(self):
        subject = self.accelerator(self.plan("tensorrt"))
        self.assertEqual(subject.infer(self.tensor), (None, None))
        self.assertEqual(self.cpu.calls, 0)
        self.assertEqual(subject.status()["active"], "")
        subject.accelerate()
        self.assertIs(subject.infer(self.tensor)[1], self.gpu)
        # The parity check only: two camera frames and the noise frame.
        self.assertEqual(self.cpu.calls, 3)

    def test_tensorrt_campaign_ends_the_worker_on_failure(self):
        subject = self.accelerator(self.plan("tensorrt"), build="failed")
        subject.accelerate()
        self.assertEqual(subject.exits, [1])
        self.assertEqual(subject.emitted[-1]["type"], "error")
        self.assertIn("could not build an engine", subject.emitted[-1]["error"])

    def test_waits_for_a_camera_frame_before_comparing(self):
        subject = self.accelerator(frames=0)
        subject.wait = lambda seconds: False  # The worker stops while it waits.
        subject.accelerate()
        self.assertEqual(subject.status()["engine"]["error"], "waiting for a camera frame to compare the cpu and tensorrt on")
        self.assertEqual(subject.status()["active"], "cpu")

    def test_stats(self):
        subject = self.accelerator(frames=3)
        stats = subject.stats()
        self.assertEqual(stats["frames_scored"], 3)
        self.assertGreaterEqual(stats["infer_ms_p95"], stats["infer_ms_p50"])

    def test_building_status_reports_its_age(self):
        subject = self.accelerator()
        subject.update(engine={"state": "building", "started_at": "2026-10-27T12:00:00Z",
                               "started": time.monotonic() - 42})
        engine = subject.status()["engine"]
        self.assertAlmostEqual(engine["elapsed_seconds"], 42, delta=1)
        self.assertNotIn("started", engine)


@unittest.skipIf(np is None, "requires numpy")
class TensorRTEngineTests(unittest.TestCase):
    """The engine wrapper against a fake TensorRT and a fake CUDA, which copy
    the bytes the way the real ones would."""

    def fakes(self, output_shape=(1, 6, 8), dtype="float32"):
        memory = {}

        class Cuda:
            def __init__(self):
                self.freed = []

            def stream(self):
                import ctypes
                return ctypes.c_void_p(7)

            def malloc(self, size):
                import ctypes
                address = ctypes.c_void_p(1000 + len(memory))
                memory[address.value] = np.zeros(size // 4, np.float32)
                return address

            def to_device(self, destination, array, stream):
                memory[destination.value][:] = array.ravel()

            def to_host(self, array, source, stream):
                array.ravel()[:] = memory[source.value]

            def synchronize(self, stream):
                pass

            def free(self, address):
                self.freed.append(address.value)

            def destroy_stream(self, stream):
                pass

        class Context:
            def __init__(self):
                self.addresses = {}

            def set_tensor_address(self, name, address):
                self.addresses[name] = address

            def execute_async_v3(self, stream):
                source = memory[self.addresses["images"]]
                memory[self.addresses["output0"]][:] = source[:48] + 1
                return True

        class Engine:
            num_io_tensors = 2
            device_memory_size_v2 = 1 << 20

            def get_tensor_name(self, i):
                return ["images", "output0"][i]

            def get_tensor_mode(self, name):
                return "in" if name == "images" else "out"

            def get_tensor_shape(self, name):
                return (1, 3, 4, 4) if name == "images" else output_shape

            def get_tensor_dtype(self, name):
                return dtype

            def create_execution_context(self):
                return Context()

        trt = types.SimpleNamespace(
            __version__="10.16.2.10", float32="float32",
            TensorIOMode=types.SimpleNamespace(INPUT="in", OUTPUT="out"),
            Logger=types.SimpleNamespace(WARNING=2),
            Runtime=lambda logger: types.SimpleNamespace(
                deserialize_cuda_engine=lambda plan: Engine() if plan == b"plan" else None))
        trt.Logger = lambda severity: object()
        trt.Logger.WARNING = 2
        return trt, Cuda()

    def test_scores_through_device_buffers(self):
        trt, cuda = self.fakes()
        engine = accelerator.TensorRTEngine(trt, cuda, b"plan", [1, 3, 4, 4], [1, 6, 8])
        tensor = np.arange(48, dtype=np.float32).reshape(1, 3, 4, 4)
        output = engine.infer(tensor)
        self.assertEqual(output.shape, (1, 6, 8))
        np.testing.assert_array_equal(output.ravel(), np.arange(48) + 1)
        self.assertEqual(engine.device_bytes(), 1 << 20)
        self.assertEqual(engine.runtime, "TensorRT 10.16.2.10")
        engine.close()
        self.assertEqual(len(cuda.freed), 2)

    def test_refuses_an_engine_of_another_shape(self):
        trt, cuda = self.fakes(output_shape=(1, 6, 9))
        with self.assertRaisesRegex(accelerator.EngineError, "shape"):
            accelerator.TensorRTEngine(trt, cuda, b"plan", [1, 3, 4, 4], [1, 6, 8])

    def test_refuses_a_plan_that_does_not_deserialize(self):
        trt, cuda = self.fakes()
        with self.assertRaisesRegex(accelerator.EngineError, "deserialize"):
            accelerator.TensorRTEngine(trt, cuda, b"other", [1, 3, 4, 4], [1, 6, 8])


if __name__ == "__main__":
    unittest.main()
