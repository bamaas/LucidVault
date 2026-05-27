FROM denoland/deno:latest
WORKDIR /app
RUN apt-get update && apt-get install -y git && rm -rf /var/lib/apt/lists/*
RUN git clone --recursive https://github.com/vrtmrz/livesync-bridge .
RUN deno install
CMD ["deno", "task", "run"]
