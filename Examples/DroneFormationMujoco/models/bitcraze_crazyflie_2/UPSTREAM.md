# Bitcraze Crazyflie 2 model provenance

- Repository: `https://github.com/google-deepmind/mujoco_menagerie.git`
- Commit: `71f066ad0be9cd271f7ed58c030243ef157af9f4`
- Source model: `bitcraze_crazyflie_2/cf2.xml` and the meshes in `bitcraze_crazyflie_2/assets/`
- License: MIT; see `LICENSE` in this directory.

The Menagerie model was converted from Bitcraze's `crazyflie_description` URDF.
`cf2.xml` and the meshes are unmodified. `drone_formation/model.py` loads the
file, copies the airframe once per drone, and replaces the upstream thrust and
body-moment actuators with four rotor actuators per drone.
