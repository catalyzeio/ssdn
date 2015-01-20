#!/bin/sh -e
# Shamelessly stolen from https://github.com/jpetazzo/pipework

if [ "$#" -ne 3 ]; then
	echo "Usage: $0 [tundev] [container] [ip]"
	exit 1
fi

TUNDEV=$1
CONTAINER=$2
TUNIP=$3

echo Injecting $TUNDEV into $CONTAINER with $TUNIP

NSPID=$(docker inspect -f '{{ .State.Pid }}' $CONTAINER)

cd /var/run/netns
rm -f $CONTAINER
ln -s /proc/$NSPID/ns/net $CONTAINER

ip link set $TUNDEV netns $CONTAINER
ip netns exec $CONTAINER \
	ifconfig $TUNDEV $TUNIP netmask 255.255.255.0
