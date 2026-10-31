# The lucy CLI image. It installs and converges server directories; it does not
# run a Minecraft server.
#
# GoReleaser stages the context as linux/amd64/lucy and linux/arm64/lucy. A
# local build without that layout falls back to the amd64 path.

FROM debian:13-slim

# lsof: running-server detection through session.lock.
# git: lucy compile.
# python3 and python3-venv: MCDReforged provisioning with --mcdr-venv.
# No JRE: Java is selected from the lock and provisioned when a server needs it.
RUN apt-get update \
  && apt-get install -y --no-install-recommends \
  ca-certificates \
  git \
  lsof \
  python3 \
  python3-venv \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd --gid 1000 lucy \
  && useradd --uid 1000 --gid 1000 --create-home --shell /bin/bash lucy \
  && install -d -o lucy -g lucy /workspace

ARG TARGETPLATFORM
COPY --chmod=0755 ${TARGETPLATFORM:-linux/amd64}/lucy /usr/local/bin/lucy

# Every container would otherwise report as a new installation.
ENV LUCY_TELEMETRY=off \
  HOME=/home/lucy

WORKDIR /workspace

ENTRYPOINT ["lucy"]
CMD ["--help"]
