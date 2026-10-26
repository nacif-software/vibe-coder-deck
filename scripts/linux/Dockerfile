# Linux builder for Vibe Coder Deck: Go, GTK4 + WebKitGTK 6.0 (Wails v3), and an X11
# test bench (Xvfb, openbox, xdotool). Used by scripts/docker-linux.sh.
FROM golang:1.26-trixie
ENV DEBIAN_FRONTEND=noninteractive
RUN sed -i 's|http://deb.debian.org|https://deb.debian.org|g' /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Retries=5 update && apt-get -o Acquire::Retries=5 install -y --no-install-recommends \
    build-essential pkg-config ca-certificates zip \
    libgtk-4-dev libwebkitgtk-6.0-dev \
    xvfb openbox xdotool x11-utils \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
