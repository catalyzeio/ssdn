package overlay

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/catalyzeio/go-core/actions"
	"github.com/catalyzeio/go-core/comm"
	"github.com/catalyzeio/taptun"
)

type L3HostTun struct {
	ip  uint32
	mtu uint16

	route  *IPv4Route
	routes *RouteTracker

	invoker *actions.Invoker

	network           *net.IPNet
	alternateNetworks []*net.IPNet

	free PacketQueue
	out  PacketQueue

	control chan struct{}
}

func NewL3HostTun(ip net.IP, mtu uint16, routes *RouteTracker, actionsDir string, network *net.IPNet, alternateNetworks []*net.IPNet) *L3HostTun {
	const tunQueueSize = tapQueueSize
	free := AllocatePacketQueue(tunQueueSize, ethernetHeaderSize+int(mtu))
	out := make(PacketQueue, tunQueueSize)

	ipVal := comm.IPv4ToInt(ip)
	return &L3HostTun{
		ip:  ipVal,
		mtu: mtu,

		route:  &IPv4Route{ipVal, 0xFFFFFFFF, out},
		routes: routes,

		invoker: actions.NewInvoker(actionsDir),

		network:           network,
		alternateNetworks: alternateNetworks,

		free: free,
		out:  out,

		control: make(chan struct{}, 1),
	}
}

func (t *L3HostTun) Start() error {
	t.invoker.Start()

	tun, err := t.createTun()
	if err != nil {
		return err
	}

	go t.service(tun)

	return nil
}

func (t *L3HostTun) Stop() {
	t.control <- struct{}{}
}

func (t *L3HostTun) LocalRoute() *IPv4Route {
	return t.route
}

func (t *L3HostTun) InboundHandler(packet *PacketBuffer) error {
	t.routes.RoutePacket(packet)
	return nil
}

func (t *L3HostTun) createTun() (*taptun.Interface, error) {
	if log.IsDebugEnabled() {
		log.Debug("Creating new tun")
	}

	const tunNameTemplate = "sl3.tun%d"
	tun, err := taptun.NewTUN(tunNameTemplate)
	if err != nil {
		return nil, err
	}

	name := tun.Name()
	log.Info("Created layer 3 tun %s", name)

	mtu := strconv.Itoa(int(t.mtu))
	var altNets []string
	for _, n := range t.alternateNetworks {
		altNets = append(altNets, n.String())
	}
	_, err = t.invoker.Execute("init", mtu, name, comm.FormatIPWithMask(t.ip, t.network.Mask), strings.Join(altNets, " "))
	if err != nil {
		tun.Close()
		return nil, err
	}

	return tun, nil
}

func (t *L3HostTun) service(tun *taptun.Interface) {
	for {
		if !ForwardL3Tun(tun, t.route, t.routes, t.free, t.out, t.control) {
			return
		}

		for {
			select {
			case <-t.control:
				return
			case <-time.After(5 * time.Second):
			}
			newTun, err := t.createTun()
			if err == nil {
				tun = newTun
				break
			}
			log.Warn("Failed to create tun: %s", err)
		}
	}
}
