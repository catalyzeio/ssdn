package overlay

type L2PeersWrapper struct {
	uplinks  *L2Uplinks
	listener *L2Listener
}

func NewL2PeersWrapper(uplinks *L2Uplinks, listener *L2Listener) *L2PeersWrapper {
	return &L2PeersWrapper{uplinks, listener}
}

func (p *L2PeersWrapper) AddPeer(url string) error {
	return p.uplinks.AddUplink(url)
}

func (p *L2PeersWrapper) DeletePeer(url string) error {
	return p.uplinks.DeleteUplink(url)
}

func (p *L2PeersWrapper) ListPeers() map[string]*PeerDetails {
	result := p.uplinks.ListUplinks()
	if p.listener != nil {
		for k, v := range p.listener.ListDownlinks() {
			result[k] = v
		}
	}
	return result
}
