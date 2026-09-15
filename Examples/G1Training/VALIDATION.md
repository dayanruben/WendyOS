# Source import validation — 2026-09-15

The 28 Python source/test files were copied from the local RTX 5090 deployment
and its contact-audit dependency. SHA256 checks verified the downloaded bytes.
The original and packaged hashes are in `source-provenance.json`.

## Completed checks

| Check | Result |
| --- | --- |
| Local regression suite, Python 3.10 / PyTorch 2.10.0 / MuJoCo 3.12.0 | 36 passed |
| Same packaged regression suite on the 5090 host, PyTorch 2.8.0+cu128 / MuJoCo 3.12.0 | 36 passed |
| Trainer `--help`, both environments | Exit 0; configurable data/cache roots present |
| Recorder `--help`, both environments | Exit 0 |
| Staged whitespace check | Passed |

The suite covers reference-bank hash/order and held-out-split enforcement,
sensor packing, recurrent migration and sequence parity, episode memory resets,
reward calculations, release timing and correction-controller behavior. An
initial packaging pass found a missing `release_audit.py` dependency in a
regression test; that source module was included before the passing runs.

The 5090 checks ran in an isolated temporary directory with
`CUDA_VISIBLE_DEVICES` empty. They did not start training, render an episode,
alter the running deployment or command a robot. The imported package has not
been subjected to a new end-to-end CUDA training run or full-task qualification.
The datasets, pretrained checkpoint and NVIDIA runtime remain external inputs.
