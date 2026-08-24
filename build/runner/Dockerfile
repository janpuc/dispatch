FROM --platform=$BUILDPLATFORM golang:1.27-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/dispatch-run ./cmd/dispatch-run

FROM node:22-bookworm-slim
ARG CLAUDE_CODE_VERSION=latest
ARG KUBECTL_VERSION=v1.36.3
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        jq \
        openssh-client \
        procps \
        ripgrep \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSLo /usr/local/bin/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/$(dpkg --print-architecture)/kubectl" \
    && chmod 0755 /usr/local/bin/kubectl
RUN npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
    && npm cache clean --force
COPY --from=build /out/dispatch-run /usr/local/bin/dispatch-run
USER node
ENTRYPOINT ["dispatch-run"]
