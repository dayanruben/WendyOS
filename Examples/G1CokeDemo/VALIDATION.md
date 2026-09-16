# Validation

Checked on 16 September 2026 with the supplied pinned assets.

## Local checks

- The asset importer verified all 17 files, about 201 MiB. Binary assets remain ignored by Git.
- Python regressions passed, 30 tests. The ROS integration test skips outside a ROS environment.
- All 100 sealed inference comparisons passed at absolute and relative tolerance `1e-5`. Maximum action error was `1.0728836e-5`; maximum joint-target error was `1.1920929e-7` radians on macOS ARM64.
- The target-driven scene matched the supplied expert physics for 100 steps. Camera rendering uses separate MuJoCo data so it preserves the physics solver state.
- The complete 2,400-step expert replay lifted the can `0.3362592 m` and ended `0.0099454 m` horizontally from the marker.
- The live RGB camera produced valid frames. JavaScript syntax checks passed. Browser interaction and visual layout were not tested because no browser was available.

## ROS transport

The integration test passed in the WendyOS `g1-sim` VM using ROS 2 Humble and CycloneDDS. It exercised real trajectory delivery, joint and camera messages, depth bytes, camera calibration, IMU, odometry, transforms, simulation clock, late subscribers and the reset service.

The test uses temporary files and ROS domain 77 to keep its synthetic clock separate from the application on domain 0.

## Scope

The scene is the supplied fixed-pelvis, 43-joint attempt-000001 model. It retains the original waist constraints, desk layout and Dex3 hands. The checkpoint's golden trace belongs to attempt-000159. Golden parity verifies inference against that trace; it does not establish success in the new scene.

The learned controller uses the original 7,518-step timing. A preliminary run on the expert replay's 60-second retiming failed to lift the can, so that timing is reserved for expert replay. Policy observations come directly from copied scene state and are also published over ROS. Both built-in controllers send their targets through DDS before physics applies them.

Local development uses Python 3.12 and NumPy 2.2.6. The container uses Python 3.10 and NumPy 1.26.4 to match Humble's Python and message ABI. Both use PyTorch 2.7.1 and MuJoCo 3.12.0.
