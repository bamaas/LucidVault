FROM jdxcode/mise:latest
WORKDIR /src
USER root
COPY mise.toml ./
RUN mise trust && mise install

# Docker CLI (for building/running containers from inside devcontainer)
RUN install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list \
    && apt-get update && apt-get install -y docker-ce-cli && rm -rf /var/lib/apt/lists/*

# Install bsdmainutils
RUN apt-get update && \
    apt-get install -y bsdmainutils --no-install-recommends && \
    rm -rf /var/lib/apt/lists/*

# Install sqlite3
RUN apt-get update && \
    apt-get install -y sqlite3 --no-install-recommends && \
    rm -rf /var/lib/apt/lists/*

ENTRYPOINT [""]
