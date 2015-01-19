brouted
=======
Your basic bridge-routing daemon.

How It Works
------------
Combine four parts [Linux TUN driver](https://www.kernel.org/doc/Documentation/networking/tuntap.txt), two parts [Linux bridge](https://www.kernel.org/doc/Documentation/networking/bridge.txt), and one part [Go](https://golang.org/). Sprinkle in encryption to taste.

In short, it's kind of like a VPN if you ran it over a dynamic peer-to-peer overlay using a fully-connected mesh topology. Similar in idea to [Weave](http://weave.works/), but run over L3 and closer to [tinc](http://www.tinc-vpn.org/) in implementation.

Proof-of-Concept Instructions
-----------------------------
TBD
