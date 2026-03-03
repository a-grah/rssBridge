#!/bin/bash
# Run once on the Pi to prepare it for rssBridge.
# ssh pi@raspberrypi.local "bash -s" < deploy/setup-pi.sh

set -e

sudo useradd --system --no-create-home --shell /usr/sbin/nologin rssbridge 2>/dev/null || true
sudo mkdir -p /opt/rssbridge /var/lib/rssbridge
sudo chown rssbridge:rssbridge /var/lib/rssbridge
echo "Pi setup complete."
