FROM ubuntu:22.04

RUN apt-get update && apt-get install -y --no-install-recommends \
    python3-pip python3-dev libopenblas-dev libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# ONNX Runtime GPU: NVIDIA's JetPack-6 / CUDA-12.6 ARM64 wheel.
#
# WHY CUDA 12 on a CUDA-13 (JetPack 7 / WendyOS 0.17) Orin: the CUDA-13 "sbsa" builds are
# compiled only for Thor (sm_110) / Spark (sm_121) and have NO kernels for Orin (sm_87),
# so they crash on the first GPU op ("no kernel image is available"). The CUDA-12.6 jp6
# wheels DO include sm_87, and CUDA 12.6 runs on the JetPack-7 GPU driver via backward
# compatibility.
RUN pip3 install --no-cache-dir onnxruntime-gpu \
    --index-url https://pypi.jetson-ai-lab.io/jp6/cu126/

# Bundle the CUDA-12 runtime into the image. On JetPack 7, CDI injects the CUDA *13*
# runtime, whose sonames (libcudart.so.13, ...) do not satisfy this CUDA-12 wheel
# (libcudart.so.12, ...). Ship the matching CUDA-12 libraries via pip, collect them in a
# single dir, and put that dir FIRST on the loader path so ONNX Runtime uses the CUDA-12
# copies consistently — not the CDI-injected CUDA-13 ones for any shared soname (e.g.
# libcudnn.so.9). The GPU *driver* (libcuda.so.1) still comes from CDI/host.
RUN pip3 install --no-cache-dir \
    nvidia-cuda-runtime-cu12 \
    nvidia-cuda-nvrtc-cu12 \
    nvidia-cublas-cu12 \
    nvidia-cudnn-cu12 \
    nvidia-cufft-cu12 \
    nvidia-curand-cu12 \
    && mkdir -p /opt/cuda12/lib \
    && find /usr/local/lib/python3.10/dist-packages/nvidia -name '*.so*' -exec ln -sf {} /opt/cuda12/lib/ \; \
    && echo /opt/cuda12/lib > /etc/ld.so.conf.d/000-cuda12.conf \
    && ldconfig
ENV LD_LIBRARY_PATH=/opt/cuda12/lib

# Install onnx (model building helpers) and numpy from standard PyPI.
# NumPy pinned to 1.x (the JetPack-6 wheel was compiled against NumPy 1.x).
RUN pip3 install --no-cache-dir onnx numpy==1.26.4

RUN pip3 show onnxruntime-gpu | grep Version && echo "onnxruntime-gpu installed"

WORKDIR /app
COPY app.py .
ENV PYTHONUNBUFFERED=1
CMD ["python3", "app.py"]
