import http.client
import threading
from types import SimpleNamespace
from unittest.mock import Mock

import numpy as np
import pytest

from coke_demo.access import require_loopback
from coke_demo.hil import make_inference_server
from runtime.async_vision import AsyncVisionBuffer, VisionFrame, VisionUnavailable
from runtime.inference_client import InferenceClient, RemoteCamera
from runtime.physical_policy import IntegratedPhysicalPolicyRunner, PolicyStopRequested
from runtime.shadow_service import readiness_status


@pytest.mark.parametrize("host", ["0.0.0.0", "::", "localhost", "192.0.2.1"])
def test_control_listener_rejects_nonliteral_or_remote_hosts(host):
    with pytest.raises(ValueError, match="loopback"):
        require_loopback(host)


def test_network_hil_requires_token_before_opening_socket():
    with pytest.raises(ValueError, match="COKE_HIL_TOKEN"):
        make_inference_server(None, host="0.0.0.0", port=0)


def test_lost_proposal_reply_is_not_retried():
    client = InferenceClient("http://127.0.0.1:8098")
    connection = Mock()
    connection.getresponse.side_effect = http.client.RemoteDisconnected()
    client.connection = connection
    with pytest.raises(http.client.RemoteDisconnected):
        client.request("POST", "/propose", {"sampled_at_ns": 42})
    assert connection.request.call_count == 1
    assert client.connection is None


def test_failed_deactivation_keeps_session_for_cleanup_retry():
    client = SimpleNamespace(session_id="session", request=Mock(side_effect=OSError))
    with pytest.raises(OSError):
        RemoteCamera(client).deactivate()
    assert client.session_id == "session"
    client.request.side_effect = None
    RemoteCamera(client).deactivate()
    assert client.session_id is None


def test_stream_change_discards_cached_and_inflight_embeddings():
    encoding = threading.Event()
    release = threading.Event()
    def encode(payload):
        if payload == "old-pending":
            encoding.set()
            assert release.wait(2)
        return payload
    buffer = AsyncVisionBuffer(encode, maximum_capture_age_s=1, clock_ns=lambda: 100)
    buffer.start()
    try:
        buffer.submit(VisionFrame(1, "old", 100, "old-ready"))
        with buffer._condition:
            assert buffer._condition.wait_for(lambda: buffer._processed == 1, 2)
        old = buffer.latest()
        buffer.submit(VisionFrame(2, "old", 100, "old-pending"))
        assert encoding.wait(2)
        buffer.submit(VisionFrame(0, "new", 100, "new"))
        with pytest.raises(VisionUnavailable):
            buffer.latest()
        with pytest.raises(VisionUnavailable):
            buffer.admit(old, control_at_ns=100)
        release.set()
        with buffer._condition:
            assert buffer._condition.wait_for(lambda: buffer._processed == 2, 2)
        assert buffer.latest().embedding == "new"
    finally:
        release.set()
        buffer.close()


def test_stop_during_entry_prevents_next_motion_command():
    runner = IntegratedPhysicalPolicyRunner.__new__(IntegratedPhysicalPolicyRunner)
    runner.stop_requested = threading.Event()
    runner.stop_requested.set()
    runner._publish_with_timing_retries = Mock()
    with pytest.raises(PolicyStopRequested):
        runner._publish_entry_tick("owner", 1, np.zeros(43), 1., 0.)
    runner._publish_with_timing_retries.assert_not_called()


def test_frozen_camera_is_not_ready():
    state = {"healthy": True, "identity": {}, "provider": {
        "exact_frame_synchronized": True, "target_mask_valid": True}}
    status = readiness_status(state, {"latest_capture_age_ms": 1000},
                              observed_at_unix_ns=1, revision="test")
    assert not status["perception"]["stream_synchronized"]
    assert not status["perception"]["target_mask_valid"]
