package cmd

import (
	"flag"
	"fmt"
	"net"
	"path"
        "runtime"

	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/go-core/simplelog"
	"github.com/catalyzeio/paas-orchestration/registry"

	"github.com/catalyzeio/ssdn/overlay"
	"github.com/catalyzeio/ssdn/watch"
)

func StartL3Node() {
	log := simplelog.NewLogger("l3node")

	simplelog.AddFlags()
	comm.AddListenFlags(true, 0, true)
	comm.AddTLSFlags()
	registry.AddFlags(false)
	overlay.AddTenantFlags()
	overlay.AddMTUFlag()
	overlay.AddNetworkFlag()
	overlay.AddDirFlags(true, true)
	overlay.AddPeerTLSFlags()
	ipFlag := flag.String("ip", "", "IP address for this node [required]")
	flag.Parse()

	tenant, tenantID, err := overlay.GetTenantFlags()
	if err != nil {
		fail("Invalid tenant config: %s\n", err)
	}
	log.Info("Servicing tenant: %s, tenant ID: %s", tenant, tenantID)

	mtu, err := overlay.GetMTUFlag()
	if err != nil {
		fail("Invalid MTU config: %s\n", err)
	}

	network, err := overlay.GetNetworkFlag()
	if err != nil {
		fail("Invalid network config: %s\n", err)
	}
	log.Info("Overlay network: %s", network)

	ip := net.ParseIP(*ipFlag)
	if len(ip) == 0 {
		fail("-ip is required\n")
	}
	if ip == nil {
		fail("Invalid IP: %s\n", ip)
	}
	ip = ip.To4()
	if ip == nil {
		fail("IP must be IPv4: %s\n", ip)
	}
	if !network.Contains(ip) {
		fail("Overlay network %s does not contain IP %s\n", network, ip)
	}
	log.Info("Local IP: %s", ip)

	_, confDir, err := overlay.GetDirFlags()
	if err != nil {
		fail("Invalid directory config: %s\n", err)
	}

	listenAddress, err := comm.GetListenAddress()
	if err == nil && listenAddress == nil {
		err = fmt.Errorf("-listen is required")
	}
	if err != nil {
		fail("Invalid listener config: %s\n", err)
	}

	config, err := comm.GenerateTLSConfig(true)
	if err != nil {
		fail("Invalid TLS config: %s\n", err)
	}

	routes := overlay.NewRouteTracker()

	tun := overlay.NewL3HostTun(ip, mtu, routes, path.Join(confDir, "l3node.d"), network)
	if err := tun.Start(); err != nil {
		fail("Failed to initialize host interface: %s\n", err)
	}

	peerConfig := overlay.GetPeerTLSConfig(config)
	localRoute := tun.LocalRoute()
	peers := overlay.NewL3Peers(localRoute, routes, peerConfig, mtu, tun.InboundHandler)

	listener := overlay.NewL3Listener(peers, listenAddress, config)
	if err := listener.Start(); err != nil {
		fail("Failed to start listener: %s\n", err)
	}

	peers.Start(listenAddress.PublicString())

	rc, err := registry.GenerateClient(tenant, config)
	if err != nil {
		fail("Failed to start registry client: %s\n", err)
	}
	if rc != nil {
		advertiseAddress := listenAddress.PublicString()
		rw := watch.NewRegistryWatcher(rc, "ssdn-l3", advertiseAddress, true)
		rw.Watch(peers)
	}

	/*dl := overlay.NewListener(tenant, runDir)
	if err := dl.Listen(nil, peers, routes, nil); err != nil {
		fail("Failed to start domain socket listener: %s\n", err)
	}*/
        runtime.Goexit()
}
