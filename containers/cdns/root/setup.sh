#!/bin/sh -e
export DEBIAN_FRONTEND=noninteractive

# Install djbdns, inotify-tools, and runit.
# svscan from daemontools does not respond to SIGTERM,
# which causes issues when run as a Docker container.
apt-get update
apt-get upgrade -y
apt-get install -y \
        djbdns inotify-tools runit

# initialize dns database
(cd /srv/tinydns/root && make)

# clean up unnecessary cruft
apt-get clean
rm -rf /var/lib/apt
