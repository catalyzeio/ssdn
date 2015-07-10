#!/bin/bash -e
CONFIG=/data/cdns/data

USER=nobody
LOGUSER=syslog

mkdir -p /service

# set up tinydns
tinydns-conf ${USER} ${LOGUSER} /etc/tinydns 127.0.0.2
cd /etc/tinydns/root
./add-ns ssdn 127.0.0.2
make

# set up dnscache
dnscache-conf ${USER} ${LOGUSER} /etc/dnscache 0.0.0.0
cd /etc/dnscache
echo 127.0.0.2 > root/servers/ssdn
chmod 644 root/servers/ssdn
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
#!/bin/sh -e
exec 2>&1

cd /etc/tinydns/root
while true; do
    cp ${CONFIG} .
    echo updating DNS configuration ${CONFIG}
    make
    inotifywait -q -e close_write ${CONFIG}
done
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
ln -s /etc/tinydns /service/tinydns
ln -s /etc/dnscache /service/dnscache
ln -s /etc/watch /service/watch
