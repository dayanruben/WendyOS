"""DDS queue age and publisher ownership across resets, without a ROS daemon."""

from types import SimpleNamespace

import pytest

from go2_sim.commands import ROSCommands
from go2_sim.simulation import Simulation


class Clock:
    now = 1_000_000_000_000

    def ns(self):
        return self.now

    def seconds(self):
        return self.now / 1e9

    def tick(self):
        self.now += 50_000_000


@pytest.fixture
def bus():
    clock = Clock()
    sim = Simulation(monotonic=clock.seconds)
    runtime = SimpleNamespace(sim=sim, ensure_running=lambda: None,
                              command=lambda values, token: sim.command_velocity(*values, token))
    return ROSCommands(runtime, "/unused", monotonic_ns=clock.ns, wall_ns=clock.ns), clock


def envelope(clock, gid="a" * 48):
    return {"kind": "twist", "publisher_gid": gid, "received_ns": clock.ns(),
            "source_timestamp_ns": clock.ns(), "velocity": [0.3, 0.0, 0.0]}


def test_reset_blocks_still_running_writer_even_after_explicit_regrant(bus):
    commands, clock = bus
    assert not commands.admit(envelope(clock))
    commands.grant("a" * 48)
    clock.tick()
    assert commands.admit(envelope(clock))
    commands.revoke()
    commands.runtime.sim.reset()
    clock.tick()
    assert not commands.admit(envelope(clock))
    with pytest.raises(PermissionError, match="restart"):
        commands.grant("a" * 48)
    assert not commands.admit(envelope(clock, "b" * 48))
    commands.grant("b" * 48)
    clock.tick()
    assert commands.admit(envelope(clock, "b" * 48))
    assert not commands.admit(envelope(clock))


def test_queueing_does_not_renew_command_lifetime(bus):
    commands, clock = bus
    commands.admit(envelope(clock))
    commands.grant("a" * 48)
    clock.tick()
    queued = envelope(clock)
    clock.tick()
    assert commands.admit(queued)
    assert commands.runtime.sim._last_received == queued["received_ns"] / 1e9
    for _ in range(4):
        clock.tick()
    with pytest.raises(ValueError, match="expired ingress"):
        commands.admit(queued)


def test_old_dds_sample_cannot_cross_new_grant(bus):
    commands, clock = bus
    queued = envelope(clock)
    commands.admit(queued)
    clock.tick()
    commands.grant("a" * 48)
    clock.tick()
    queued["received_ns"] = clock.ns()
    assert not commands.admit(queued)
    assert commands.runtime.sim.command.tolist() == [0.0, 0.0, 0.0]


def test_browser_and_ros_cannot_own_controller_together(bus):
    commands, clock = bus
    commands.admit(envelope(clock))
    commands.runtime.sim.arm()
    with pytest.raises(PermissionError, match="another"):
        commands.grant("a" * 48)


def test_node_labels_are_display_metadata_and_do_not_grant_control(bus):
    commands, clock = bus
    packet = envelope(clock) | {"node_name": "wendy_go2_patrol", "node_namespace": "/apps/robot"}
    assert not commands.admit(packet)
    source = commands.status()["sources"][0]
    assert source["node_name"] == "wendy_go2_patrol"
    assert source["node_namespace"] == "/apps/robot"
    assert commands.owner is None
    with pytest.raises(ValueError, match="discovered"):
        commands.grant("wendy_go2_patrol")
    commands.grant(packet["publisher_gid"])
    clock.tick()
    assert commands.admit(envelope(clock) | {"node_name": "another_name", "node_namespace": "/"})
    assert commands.owner == packet["publisher_gid"]


@pytest.mark.parametrize("metadata", [
    {"node_name": "not/a/node", "node_namespace": "/"},
    {"node_name": "<script>", "node_namespace": "/"},
    {"node_name": "bad\nname", "node_namespace": "/"},
    {"node_name": "a" * 129, "node_namespace": "/"},
    {"node_name": "app", "node_namespace": "/" + "a" * 128},
    {"node_name": "app", "node_namespace": "relative"},
    {"node_name": "app", "node_namespace": "/trailing/"},
    {"node_name": "app", "node_namespace": "/double//slash"},
    {"node_name": "app", "node_namespace": "/9invalid"},
    {"node_name": "app"}, {"node_namespace": "/"},
    {"node_name": 7, "node_namespace": "/"},
])
def test_invalid_optional_labels_never_change_command_admission(bus, metadata):
    commands, clock = bus
    commands.admit(envelope(clock))
    commands.grant("a" * 48)
    clock.tick()
    assert commands.admit(envelope(clock) | metadata)
    source = commands.status()["sources"][0]
    assert "node_name" not in source
    assert "node_namespace" not in source


def test_same_named_nodes_keep_distinct_publisher_grants_and_reset_fences(bus):
    commands, clock = bus
    metadata = {"node_name": "same_node", "node_namespace": "/"}
    commands.admit(envelope(clock, "a" * 48) | metadata)
    commands.admit(envelope(clock, "b" * 48) | metadata)
    assert len(commands.status()["sources"]) == 2
    commands.grant("a" * 48)
    clock.tick()
    assert not commands.admit(envelope(clock, "b" * 48) | metadata)
    assert commands.admit(envelope(clock, "a" * 48) | metadata)
    commands.revoke()
    commands.runtime.sim.reset()
    with pytest.raises(PermissionError, match="restart"):
        commands.grant("a" * 48)


def test_reordered_packets_cannot_replace_a_publisher_label(bus):
    commands, clock = bus
    packet = envelope(clock) | {"node_name": "new_name", "node_namespace": "/"}
    commands.admit(packet)
    assert not commands.admit(packet | {"node_name": "old_name"})
    assert commands.status()["sources"][0]["node_name"] == "new_name"


def test_optional_metadata_omission_preserves_only_the_same_endpoint_label(bus):
    commands, clock = bus
    commands.admit(envelope(clock) | {"node_name": "app", "node_namespace": "/examples"})
    clock.tick()
    commands.admit(envelope(clock))
    assert commands.status()["sources"][0]["node_name"] == "app"
    assert commands.status()["sources"][0]["node_namespace"] == "/examples"
    commands.admit(envelope(clock, "b" * 48))
    assert "node_name" not in commands.status()["sources"][1]
    clock.tick()
    commands.admit(envelope(clock) | {"node_name": "invalid/name", "node_namespace": "/"})
    assert "node_name" not in commands.status()["sources"][0]
