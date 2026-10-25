# gatekeeper: small Go daemon that fronts the sleeping devbox container,
# wakes it on traffic, serves a loading page until backends answer and stops
# it again after idle. Built on the build platform and cross-compiled.
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS gatekeeper-build
ARG TARGETARCH
COPY gatekeeper/ /src/gatekeeper/
WORKDIR /src/gatekeeper
RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH}" \
    go build -trimpath -ldflags="-s -w" -buildvcs=false -o /out/gatekeeper .

FROM ubuntu:26.04
ARG TARGETARCH

# Base
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends \
         build-essential \
         software-properties-common \
         gnupg2 \
         curl \
         zip \
         unzip \
         zstd \
         sudo \
         git \
         openssh-client \
         openssh-server \
         pipx \
         nano \
         htop \
    && rm -rf /var/lib/apt/lists/* \
    && { echo 'Defaults secure_path="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/devbox/pi/bin:/opt/devbox/pi/node/bin"'; \
         echo 'ubuntu ALL=(root) NOPASSWD:ALL'; } > /etc/sudoers.d/ubuntu \
    && chmod 0440 /etc/sudoers.d/ubuntu \
    && rm -f /etc/ssh/ssh_host_*

# vim
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends \
         vim \
    && rm -rf /var/lib/apt/lists/*

# git lfs
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    apt-get update -y \
    && apt-get install -y --no-install-recommends \
      git-lfs \
    && git lfs version \
    && rm -rf /var/lib/apt/lists/*

# glab CLI
ARG GLAB_VERSION="1.120.0"
RUN curl -fL "https://gitlab.com/gitlab-org/cli/-/releases/v${GLAB_VERSION}/downloads/glab_${GLAB_VERSION}_linux_${TARGETARCH}.deb" -o /tmp/glab.deb \
    && dpkg -i /tmp/glab.deb \
    && rm /tmp/glab.deb \
    && glab --version

# gh CLI
RUN install -d -m 0755 /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh \
    && gh --version \
    && rm -rf /var/lib/apt/lists/*

# multirun
ARG MULTIRUN_VERSION=1.1.3
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) ARCH=x86_64 ;; \
      arm64) ARCH=aarch64 ;; \
      *) echo "Unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -L -o - "https://github.com/nicolas-van/multirun/releases/download/${MULTIRUN_VERSION}/multirun-${ARCH}-linux-gnu-${MULTIRUN_VERSION}.tar.gz" \
      | tar -xz -C /usr/local/bin multirun; \
    chmod +x /usr/local/bin/multirun; \
    multirun -h

# caddy
ARG CADDY_VERSION="2.11.7"
COPY dashboard/index.template.html /opt/devbox/dashboard/index.template.html
COPY dashboard/icons/ /srv/dashboard/icons/
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) ARCH=x86_64 ;; \
      arm64) ARCH=arm64 ;; \
      *) echo "Unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fL "https://github.com/caddyserver/caddy/releases/download/v${CADDY_VERSION}/caddy_${CADDY_VERSION}_linux_${ARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin caddy; \
    caddy version \
    && chown -R ubuntu:ubuntu /srv/dashboard
COPY caddy/Caddyfile /opt/devbox/Caddyfile
COPY ssh/sshd_config.d/10-devbox.conf /etc/ssh/sshd_config.d/10-devbox.conf

# ttyd
ARG TTYD_VERSION="1.7.7"
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
        --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    set -eux; \
    case "${TARGETARCH}" in \
      amd64) ARCH=x86_64 ;; \
      arm64) ARCH=aarch64 ;; \
      *) echo "Unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fL -o /usr/local/bin/ttyd "https://github.com/tsl0922/ttyd/releases/download/${TTYD_VERSION}/ttyd.${ARCH}"; \
    chmod +x /usr/local/bin/ttyd; \
    ttyd --version \
    && apt-get update -y \
    && add-apt-repository ppa:trzsz/ppa \
    && apt-get update \
    && apt-get install --no-install-recommends \
         trzsz \
    && rm -rf /var/lib/apt/lists/*

# ngrok
ARG NGROK_VERSION="3.39.11"
RUN curl -fL "https://ngrok-agent.s3.amazonaws.com/pool/main/n/ngrok/ngrok_${NGROK_VERSION}-0_${TARGETARCH}.deb" -o /tmp/ngrok.deb \
    && dpkg -i /tmp/ngrok.deb \
    && rm /tmp/ngrok.deb \
    && ngrok version

ARG ASDF_VERSION="0.20.2"
ENV PATH="$PATH:/data/asdf/shims" \
    ASDF_CONFIG_FILE="/etc/asdf/config" \
    ASDF_DIR="/opt/asdf" \
    ASDF_DATA_DIR="/data/asdf"
COPY asdf/config.ini /etc/asdf/config
RUN set -eux; \
    curl -L "https://github.com/asdf-vm/asdf/releases/download/v${ASDF_VERSION}/asdf-v${ASDF_VERSION}-linux-${TARGETARCH}.tar.gz" \
      | tar -xz -C /usr/local/bin asdf; \
    chmod +x /usr/local/bin/asdf; \
    asdf --version \
    && asdf plugin add maven  https://github.com/halcyon/asdf-maven.git \
    && asdf plugin add nodejs https://github.com/asdf-vm/asdf-nodejs.git \
    && asdf plugin add python https://github.com/danhper/asdf-python.git \
    && asdf plugin add golang https://github.com/asdf-community/asdf-golang.git \
    && asdf plugin add gradle https://github.com/rfrancis/asdf-gradle.git \
    && asdf plugin add poetry https://github.com/asdf-community/asdf-poetry.git \
    && asdf plugin add rust https://github.com/code-lever/asdf-rust.git

USER root

# code-server
ARG CODER_SERVER_VERSION="4.140.0"
RUN curl -L "https://github.com/coder/code-server/releases/download/v${CODER_SERVER_VERSION}/code-server_${CODER_SERVER_VERSION}_${TARGETARCH}.deb" -o /tmp/coder-server.deb \
    && dpkg -i /tmp/coder-server.deb \
    && code-server --help

# OpenCode
ARG OPENCODE_VERSION="1.18.30"
RUN curl -fsSL https://opencode.ai/install \
        | sed 's|INSTALL_DIR=.*|INSTALL_DIR=${OPENCODE_INSTALL_DIR:-/usr/local/bin}|' \
        | OPENCODE_INSTALL_DIR="/usr/local/bin" bash -s -- --version "${OPENCODE_VERSION}" \
    && opencode --version

# pi coding agent
ARG NODE_VERSION="22.23.3"
ARG PI_AGENT_VERSION="1.0.2"
ARG PI_WEB_VERSION="1.202610.1"
ENV PI_PREFIX="/opt/devbox/pi" \
    PI_WEB_CONFIG="/data/pi-web/config.json" \
    PI_WEB_DATA_DIR="/data/pi-web" \
    PI_WEB_PORT="8504"
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt/lists,sharing=locked \
    --mount=type=cache,target=/root/.npm,sharing=locked \
    set -eux; \
    apt-get update \
    && apt-get install -y --no-install-recommends python3 xz-utils ripgrep \
    && rm -rf /var/lib/apt/lists/*; \
    case "${TARGETARCH}" in \
      amd64) NODE_ARCH=x64 ;; \
      arm64) NODE_ARCH=arm64 ;; \
      *) echo "Unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    install -d -m 0755 "${PI_PREFIX}/node"; \
    curl -fL "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz" \
      | tar -xJ --strip-components=1 -C "${PI_PREFIX}/node"; \
    PATH="${PI_PREFIX}/bin:${PI_PREFIX}/node/bin:${PATH}"; \
    export PATH; \
    node --version; \
    npm --version; \
    npm install -g --prefix "${PI_PREFIX}" --ignore-scripts \
      "@earendil-works/pi-coding-agent@${PI_AGENT_VERSION}"; \
    npm install -g --prefix "${PI_PREFIX}" \
      "@jmfederico/pi-web@${PI_WEB_VERSION}"; \
    pi --version; \
    pi-web --version
COPY pi/config.json /opt/devbox/pi/config.json
ENV PATH="${PATH}:/opt/devbox/pi/bin:/opt/devbox/pi/node/bin"

# prep SSH and fix up ownership
RUN install -d -m 0755 /data/ssh \
    && chown -R ubuntu:ubuntu /data

EXPOSE 8022 \
       8080

# copy entrypoints
COPY entrypoints/ /opt/devbox/
COPY --from=gatekeeper-build /out/gatekeeper /opt/devbox/gatekeeper
ENTRYPOINT ["multirun","/opt/devbox/caddy", "/opt/devbox/code-server", "/opt/devbox/opencode-server", "/opt/devbox/pi-web-sessiond", "/opt/devbox/pi-web-server", "/opt/devbox/ttyd", "/opt/devbox/sshd"]

USER ubuntu
WORKDIR /home/ubuntu

