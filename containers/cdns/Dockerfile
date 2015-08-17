FROM ubuntu:14.04

ADD root.tar /
RUN (bash -e /setup.sh && rm /setup.sh)

CMD ["/sbin/daemontools-init"]
