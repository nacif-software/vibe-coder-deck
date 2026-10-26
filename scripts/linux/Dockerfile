# Linux builder for Vibe Coder Deck: Go, GTK3 + WebKit2GTK (Wails), and an X11
# test bench (Xvfb, openbox, xdotool). Used by scripts/docker-linux.sh.
FROM golang:1.26-bookworm
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get -o Acquire::Retries=5 update && apt-get -o Acquire::Retries=5 install -y --no-install-recommends \
    build-essential pkg-config ca-certificates zip \
    libgtk-3-dev libwebkit2gtk-4.1-dev \
    xvfb openbox xdotool x11-utils \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
