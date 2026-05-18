FROM jdxcode/mise@sha256:9018ae3c83379d46a0a495ff1b7a5231a488218788ee2eb38bd6be3e5aa081ab AS builder
WORKDIR /src
COPY .mise/config.toml .mise.toml
ENV MISE_ENABLE_TOOLS="go"
RUN mise trust && mise install
COPY go.mod go.sum ./
RUN mise exec -- go mod download
COPY . .
RUN mise run build:binary

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group
COPY --from=builder /src/bin/lucidvault /lucidvault
USER nobody:nogroup
ENV VAULT_PATH=/vault
ENTRYPOINT ["/lucidvault"]
