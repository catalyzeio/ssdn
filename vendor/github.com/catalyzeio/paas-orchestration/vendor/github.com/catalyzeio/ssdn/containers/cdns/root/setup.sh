#!/bin/bash -e
export DEBIAN_FRONTEND=noninteractive

CONFIG=/data/cdns/data

USER=nobody
LOGUSER=syslog

# update and upgrade packages
apt-get update
apt-get upgrade -y

# install djbdns and inotify-tools
apt-get install -y \
        djbdns inotify-tools

# create the service directory
mkdir -p /etc/service

# set up tinydns
tinydns-conf ${USER} ${LOGUSER} /etc/tinydns 127.0.0.2
cd /etc/tinydns/root
cat <<EOF > data
# use a short NXDOMAIN TTL for the SOA record
Zinternal:a.ns.internal:owner.internal::::::5

# set up NS record for local tinydns server
.internal:127.0.0.2:a:259200

# .internal records
EOF
make

# set up dnscache
dnscache-conf ${USER} ${LOGUSER} /etc/dnscache 0.0.0.0
cd /etc/dnscache
# delegate .internal to tinydns
echo 127.0.0.2 > root/servers/internal
chmod 644 root/servers/internal
# forward everything else to our favorite public servers
cat <<EOF > root/servers/@
8.8.8.8
8.8.4.4
EOF
echo 1 > env/FORWARDONLY
# allow querying from any private IP
touch root/ip/192.168
touch root/ip/10
for (( octet = 16; octet < 32; octet++ )); do
    touch root/ip/172.${octet}
done

# set up watch utility
mkdir -p /etc/watch
cd /etc/watch
chmod 3755 .
cat <<EOF > run
#!/usr/bin/env python3
import os
import shutil
import subprocess

start_delimiter = '## START WATCH UPDATE ##'
end_delimiter = '## END WATCH UPDATE ##'

os.chdir('/etc/tinydns/root')

with open('${CONFIG}', 'r') as pipe:
    while True:
        for line in pipe:
            if line.strip() == start_delimiter:
                break
        with open('data.new', 'wb') as output:
            for line in pipe:
                if line.strip() == end_delimiter:
                    break
                output.write('{}\n'.format(line).encode('utf-8'))

        print('Updating DNS configuration ${CONFIG}')

        try:
            shutil.copy('data.new', 'data')
        except (IOError, shutil.Error) as e:
            print('Failed to copy the DNS configuration: {}'.format(e))
            continue

        try:
            subprocess.call(['make'])
        except OSError as e:
            print('Failed to run make: {}'.format(e))
            continue

EOF
chmod 755 run

mkdir -p log
cat <<EOF > log/run
#!/bin/sh
exec setuidgid ${LOGUSER} multilog t ./main
EOF
chmod 755 log/run
mkdir -p log/main
chown ${LOGUSER}:${LOGUSER} log/main

# configure all to start via svscan
ln -s /etc/tinydns /etc/service/tinydns
ln -s /etc/dnscache /etc/service/dnscache
ln -s /etc/watch /etc/service/watch

# clean up unnecessary cruft
apt-get clean
rm -rf /var/lib/apt
