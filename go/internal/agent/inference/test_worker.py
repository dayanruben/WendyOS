import threading
import unittest

from worker import StreamBytes


class StreamTests(unittest.TestCase):
    def test_byte_order_across_camera_chunks(self):
        stream = StreamBytes()
        stream.feed(b"abc")
        stream.feed(b"def")
        self.assertEqual(stream.read(2), b"ab")
        self.assertEqual(stream.read(100), b"c")
        self.assertEqual(stream.read(3), b"def")
        self.assertEqual(stream.pending, 0)
        self.assertEqual(stream.read(0), b"")

    def test_bounded_encoded_queue(self):
        stream = StreamBytes()
        self.assertTrue(stream.feed(b"x" * (8 << 20)))
        self.assertFalse(stream.feed(b"y"))
        stream.stop()
        self.assertEqual(stream.pending, 0)
        self.assertFalse(stream.feed(b"z"))

    def test_stop_unblocks_libav_read(self):
        stream = StreamBytes()
        result = []
        reader = threading.Thread(target=lambda: result.append(stream.read(4096)))
        reader.start()
        stream.stop()
        reader.join(timeout=1)
        self.assertFalse(reader.is_alive())
        self.assertEqual(result, [b""])


class WebMLateJoinTests(unittest.TestCase):
    def test_cached_initialization_decodes_late_chunks_and_resets(self):
        import importlib.util
        import pathlib
        import shutil
        import subprocess
        import tempfile
        import time
        from worker import Decoder

        if importlib.util.find_spec("av") is None or shutil.which("ffmpeg") is None:
            self.skipTest("requires PyAV and ffmpeg for real WebM decoding")
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "stream.webm"
            subprocess.run([
                "ffmpeg", "-v", "error", "-f", "lavfi", "-i",
                "testsrc2=size=96x64:rate=10", "-t", "12", "-c:v", "libvpx",
                "-g", "10", "-f", "webm", "-live", "1", str(path),
            ], check=True)
            data = path.read_bytes()
        header = data[:data.index(bytes.fromhex("1f43b675"))]
        # Neither join is aligned to a container header. A new decoder models
        # both a late subscriber and a reset after an overflowing input queue.
        for generation, offset in enumerate((len(data) // 3 + 19, len(data) // 2 + 17), 1):
            decoder = Decoder("camera", generation, "vp8", header)
            try:
                self.assertTrue(decoder.stream.feed(data[offset:]))
                deadline = time.monotonic() + 5
                while time.monotonic() < deadline:
                    with decoder.lock:
                        frame = decoder.latest
                    if frame is not None:
                        break
                    time.sleep(0.01)
                self.assertIsNotNone(frame, "late WebM subscriber never decoded")
                self.assertEqual((frame[1].width, frame[1].height), (96, 64))
            finally:
                decoder.stop()


if __name__ == "__main__":
    unittest.main()
