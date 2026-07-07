FROM ubuntu:22.04

# Install system dependencies and native libraries
# Using apt for system libraries, pip for Python packages to avoid conflicts
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3-pip \
    python3-dev \
    libopenblas-dev \
    libblas-dev \
    liblapack-dev \
    gfortran \
    cmake \
    build-essential \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# PyTorch: NVIDIA's JetPack-6 / CUDA-12.6 ARM64 wheels.
#
# WHY CUDA 12 on a CUDA-13 (JetPack 7 / WendyOS 0.17) Orin: the CUDA-13 "sbsa" wheels are
# compiled only for Thor (sm_110) / Spark (sm_121) and have NO kernels for Orin (sm_87),
# so they load but crash on the first GPU op with "no kernel image is available for
# execution on the device". The CUDA-12.6 jp6 wheels DO include sm_87, and CUDA 12.6 runs
# on the JetPack-7 GPU driver via backward compatibility.
RUN pip3 install --no-cache-dir \
    torch==2.8.0 \
    torchvision==0.23.0 \
    torchaudio==2.8.0 \
    --index-url https://pypi.jetson-ai-lab.io/jp6/cu126/

# Bundle the CUDA-12 runtime into the image. On JetPack 7, CDI injects the CUDA *13*
# runtime, whose sonames (libcudart.so.13, ...) do not satisfy these CUDA-12 wheels
# (libcudart.so.12, ...). Ship the matching CUDA-12 libraries via pip, collect them in a
# single dir, and put that dir FIRST on the loader path so torch uses the CUDA-12 copies
# consistently — not the CDI-injected CUDA-13 ones for any shared soname (e.g.
# libcudnn.so.9). The GPU *driver* (libcuda.so.1) still comes from CDI/host.
RUN pip3 install --no-cache-dir \
    nvidia-cuda-runtime-cu12 \
    nvidia-cuda-nvrtc-cu12 \
    nvidia-cuda-cupti-cu12 \
    nvidia-cublas-cu12 \
    nvidia-cudnn-cu12 \
    nvidia-cufft-cu12 \
    nvidia-curand-cu12 \
    nvidia-cusolver-cu12 \
    nvidia-cusparse-cu12 \
    nvidia-cusparselt-cu12 \
    nvidia-nccl-cu12 \
    nvidia-nvtx-cu12 \
    nvidia-nvjitlink-cu12 \
    && mkdir -p /opt/cuda12/lib \
    && find /usr/local/lib/python3.10/dist-packages/nvidia -name '*.so*' -exec ln -sf {} /opt/cuda12/lib/ \; \
    && echo /opt/cuda12/lib > /etc/ld.so.conf.d/000-cuda12.conf \
    && ldconfig
ENV LD_LIBRARY_PATH=/opt/cuda12/lib

# Install scientific Python packages with pinned numpy version
# (the JetPack-6 PyTorch wheel was compiled against NumPy 1.x)
RUN pip3 install --no-cache-dir \
    numpy==1.26.4 \
    scipy \
    matplotlib
# Verify we got the correct PyTorch version (will fail build if wrong)
# Use pip show instead of import to avoid CUDA library loading during build
RUN pip3 show torch | grep "Version: 2.8.0" && echo "✓ Correct PyTorch version installed"

# Set working directory and copy application code
WORKDIR /app
COPY gpu-test.py .

# Create a non-root user
# NOTE: Commented out for now - CUDA memory manager needs root access to /dev/nvmap
# RUN useradd --create-home --shell /bin/bash app && \
#     chown -R app:app /app
# USER app

# Set environment variables
ENV PYTHONUNBUFFERED=1

# Default command - run bash to keep container alive
# CMD ["bash", "-c", "while true; do sleep 1000; done"]
CMD ["python3", "gpu-test.py"]
