FROM python:3.12-slim-bookworm

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    DRONE_FORMATION_HOST=0.0.0.0 \
    DRONE_FORMATION_PORT=8879

WORKDIR /app

COPY requirements.txt ./requirements.txt
RUN python -m pip install --no-cache-dir -r requirements.txt

COPY drone_formation ./drone_formation
COPY models ./models

EXPOSE 8879

CMD ["python", "-m", "drone_formation.server"]
