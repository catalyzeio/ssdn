FROM ubuntu:14.04

ADD root.tar /
RUN (sh -e /setup.sh && rm /setup.sh)

CMD ["/usr/sbin/runsvdir-start"]
