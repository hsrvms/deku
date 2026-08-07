# syntax=docker/dockerfile:1.7

# Keep runtime versions explicit so upgrades are reviewable and cached independently.
ARG GO_IMAGE=golang:1.26.5-bookworm
ARG NODE_IMAGE=node:24.18.0-bookworm-slim
ARG DEBIAN_IMAGE=debian:bookworm-slim

FROM ${GO_IMAGE} AS go-tools
ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local

ARG GOLANGCI_LINT_VERSION=v2.12.2
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}

ARG DELVE_VERSION=v1.27.0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/go-delve/delve/cmd/dlv@${DELVE_VERSION}

ARG GOPLS_VERSION=v0.23.0
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install golang.org/x/tools/gopls@${GOPLS_VERSION}

ARG STATICCHECK_VERSION=2026.1
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install honnef.co/go/tools/cmd/staticcheck@${STATICCHECK_VERSION}

FROM ${NODE_IMAGE} AS node-tools
ARG PI_VERSION=0.84.0
RUN npm install --global --ignore-scripts \
        @earendil-works/pi-coding-agent@${PI_VERSION} \
    && npm cache clean --force

FROM ${DEBIAN_IMAGE}

ARG USERNAME=dev
ARG USER_UID=1000
ARG USER_GID=1000

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        bash \
        ca-certificates \
        curl \
        fd-find \
        gcc \
        gh \
        git \
        vim \
        iproute2 \
        iputils-ping \
        jq \
        less \
        libc6-dev \
        lsof \
        make \
        netcat-openbsd \
        openssh-client \
        procps \
        ripgrep \
        unzip \
        wget \
    && rm -rf /var/lib/apt/lists/* \
    && ln -s /usr/bin/fdfind /usr/local/bin/fd \
    && groupadd --gid "${USER_GID}" "${USERNAME}" \
    && useradd --uid "${USER_UID}" --gid "${USER_GID}" --create-home --shell /bin/bash "${USERNAME}" \
    && install -d -o "${USER_UID}" -g "${USER_GID}" /workspace /home/${USERNAME}/.cache \
    && install -d -m 0777 /go/pkg/mod /go/build-cache \
    && printf '%s\n' \
        'export GOPATH=/go' \
        'export GOMODCACHE=/go/pkg/mod' \
        'export GOCACHE=/go/build-cache' \
        'export GOTOOLCHAIN=local' \
        'export PATH=/usr/local/go/bin:/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin' \
        > /etc/profile.d/devcontainer.sh

COPY --from=go-tools /usr/local/go/ /usr/local/go/
COPY --from=go-tools /go/bin/ /usr/local/bin/
COPY --from=node-tools /usr/local/ /usr/local/
RUN printf '%s\n' \
        '# ~/.bashrc — color setup for interactive and non-interactive shells' \
        '' \
        '# Bail out early when sourced by a non-bash shell (e.g. dash via ENV).' \
        'if [ -z "${BASH_VERSION:-}" ]; then' \
        '  return 0 2>/dev/null || exit 0' \
        'fi' \
        '' \
        '# Ensure TERM supports color so programs emit escape sequences.' \
        'if [ -z "${TERM:-}" ] || [ "$TERM" = dumb ]; then' \
        '  export TERM=xterm-256color' \
        'fi' \
        '' \
        '# --- LS_COLORS -----------------------------------------------------------' \
        'if command -v dircolors >/dev/null 2>&1; then' \
        '  eval "$(dircolors -b)"' \
        'fi' \
        '' \
        '# --- Color aliases (always defined; expand_aliases is on for interactive) ---' \
        "alias ls='ls --color=auto'" \
        "alias dir='dir --color=auto'" \
        "alias vdir='vdir --color=auto'" \
        "alias grep='grep --color=auto'" \
        "alias fgrep='fgrep --color=auto'" \
        "alias egrep='egrep --color=auto'" \
        "alias diff='diff --color=auto'" \
        '' \
        '# --- Less with colour passthrough -----------------------------------------' \
        'export LESS=-R' \
        'export LESSOPEN="|/usr/bin/lesspipe %s" 2>/dev/null' \
        '' \
        '# --- Interactive-only settings --------------------------------------------' \
        'if [[ $- == *i* ]]; then' \
        '  # Enable alias expansion (already on for interactive, but be explicit)' \
        '  shopt -s expand_aliases' \
        '' \
        '  # Coloured prompt' \
        '  if [ "$(id -u)" -eq 0 ]; then' \
        '    PS1='"'"'\[\033[01;31m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '"'" \
        '  else' \
        '    PS1='"'"'\[\033[01;32m\]\u@\h\[\033[00m\]:\[\033[01;34m\]\w\[\033[00m\]\$ '"'" \
        '  fi' \
        '' \
        '  # Append to history rather than overwrite' \
        '  shopt -s histappend' \
        '  HISTSIZE=10000' \
        '  HISTFILESIZE=20000' \
        '  HISTCONTROL=ignoreboth:erasedups' \
        'fi' \
        > /home/${USERNAME}/.bashrc \
    && chown ${USER_UID}:${USER_GID} /home/${USERNAME}/.bashrc

ENV DEBIAN_FRONTEND= \
    HOME=/home/${USERNAME} \
    GOPATH=/go \
    GOMODCACHE=/go/pkg/mod \
    GOCACHE=/go/build-cache \
    GOTOOLCHAIN=local \
    BASH_ENV=/home/${USERNAME}/.bashrc \
    ENV=/home/${USERNAME}/.bashrc \
    PATH=/usr/local/go/bin:/go/bin:/usr/local/bin:/usr/local/sbin:/usr/sbin:/usr/bin:/sbin:/bin

USER ${USERNAME}
WORKDIR /workspace
CMD ["sleep", "infinity"]
