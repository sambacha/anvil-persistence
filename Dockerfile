# syntax=docker/dockerfile:1
FROM golang:1.19-bullseye-slim
ARG VERSION="local"
ARG REVISION="none"
ARG GITHUB_WORKFLOW="none"
ARG GITHUB_RUN_ID="none"
ARG WORKSPACE_DIR=/app


SHELL ["/bin/bash", "-c"]

RUN apt-get update && apt-get install -qqy --no-install-recommends \
    git \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get purge -y --auto-remove -o APT::AutoRemove::RecommendsImportant=false

RUN curl -L https://foundry.paradigm.xyz | bash

RUN /root/.foundry/bin/foundryup

WORKDIR /app

RUN mkdir -p /app/data && mkdir -p /opt/foundry/cache

ONBUILD ARG GONOPROXY
ONBUILD ARG GONOSUMDB

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /anvil-persistence

RUN git config --global --add safe.directory ${WORKSPACE_DIR}

# 8545 is Standard Port
# 8180 is OpenEthereum
# 3001 is a fallback port
EXPOSE 8545/tcp
EXPOSE 8545/udp
EXPOSE 8180
EXPOSE 3001/tcp

STOPSIGNAL SIGQUIT

CMD [ "/anvil-persistence", "-command=/root/.foundry/bin/anvil", "-file=data/anvil_state.txt", "-host=0.0.0.0" ]


LABEL org.opencontainers.image.url==${URL}
LABEL org.opencontainers.image.documentation=${URL}
LABEL org.opencontainers.image.source==${URL}
LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${REVISION}
LABEL org.opencontainers.image.vendor="Foundry"
LABEL org.opencontainers.image.licenses="Apache-2.0"
LABEL github.workflow=${GITHUB_WORKFLOW}
LABEL github.runid=${GITHUB_RUN_ID}
