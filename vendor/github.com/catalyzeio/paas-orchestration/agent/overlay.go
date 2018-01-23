package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/catalyzeio/go-core/comm"
	"github.com/hoisie/mustache"
)

const (
	OverlayDirsMode    = 0755
	OverlayScriptMode  = 0755
	OverlayTLSFileMode = 0600

	OverlayTenantLabel   = "io.catalyze.ssdn.tenant"
	OverlayIPLabel       = "io.catalyze.ssdn.ip"
	OverlayServicesLabel = "io.catalyze.ssdn.services"

	tenantIDLength = 15
)

type tenantOverlay struct {
	initialized bool
	network     *net.IPNet
	subnet      *net.IPNet

	pool *comm.IPPool
	jobs map[string]uint32

	gatewayIP net.IP
	dnsIP     net.IP
}

func newTenantOverlay(network, subnet *net.IPNet) (*tenantOverlay, error) {
	netVal, maskVal, err := comm.NetworkToInt(subnet)
	if err != nil {
		return nil, err
	}

	pool := comm.NewIPPool(netVal, maskVal)
	gatewayVal, err := pool.AcquireFromOffset(-1)
	if err != nil {
		return nil, err
	}
	gatewayIP := comm.IntToIP(gatewayVal)

	dnsVal, err := pool.AcquireFromOffset(-2)
	if err != nil {
		return nil, err
	}
	dnsIP := comm.IntToIP(dnsVal)

	return &tenantOverlay{
		network: network,
		subnet:  subnet,

		pool: pool,
		jobs: make(map[string]uint32),

		gatewayIP: gatewayIP,
		dnsIP:     dnsIP,
	}, nil
}

func (t *tenantOverlay) reserve(jobID string, network, subnet *net.IPNet) (string, error) {
	if err := t.checkNetwork(network, subnet); err != nil {
		return "", nil
	}

	ipVal, err := t.pool.Next()
	if err != nil {
		return "", err
	}
	t.jobs[jobID] = ipVal
	ip := comm.IntToIP(ipVal)
	return ip.String(), nil
}

func (t *tenantOverlay) restore(jobID string, ip string, network, subnet *net.IPNet) error {
	if err := t.checkNetwork(network, subnet); err != nil {
		return err
	}

	ipVal, err := t.pool.AcquireFromString(ip)
	if err != nil {
		return err
	}
	t.jobs[jobID] = ipVal
	return nil
}

func (t *tenantOverlay) release(jobID string) (bool, error) {
	jobs := t.jobs
	ipVal, present := jobs[jobID]
	if !present {
		return false, fmt.Errorf("no overlay network for job %s", jobID)
	}
	t.pool.Release(ipVal)
	delete(jobs, jobID)
	return len(jobs) == 0, nil
}

func (t *tenantOverlay) checkNetwork(network, subnet *net.IPNet) error {
	if !netEquals(network, t.network) {
		return fmt.Errorf("network %s does match existing network %s", network, t.network)
	}
	if !netEquals(subnet, t.subnet) {
		return fmt.Errorf("subnet %s does match existing subnet %s", network, t.network)
	}
	return nil
}

func netEquals(net1, net2 *net.IPNet) bool {
	if !net1.IP.Equal(net2.IP) {
		return false
	}
	if !bytes.Equal(net1.Mask, net2.Mask) {
		return false
	}
	return true
}

type tlsSettings struct {
	ca   string
	cert string
	key  string

	peerName string
}

type OverlayManager struct {
	dockerHost string

	templatesDir string
	stateDir     string
	serviceDir   string

	registryURL    string
	registryCA     string
	serviceAddress string

	network *net.IPNet
	subnet  *net.IPNet

	mutex   sync.Mutex
	tenants map[string]*tenantOverlay
	uid     string
	gid     string
}

func NewOverlayManager(dockerHost string, templatesDir, stateDir, serviceDir, registryURL, registryCA, serviceAddress string, network, subnet *net.IPNet, defaultUID, defaultGID string) (*OverlayManager, error) {
	if network != nil && subnet == nil {
		return nil, fmt.Errorf("overlay configuration missing subnet")
	}
	if subnet != nil && network == nil {
		return nil, fmt.Errorf("overlay configuration missing network")
	}
	absStateDir, err := filepath.Abs(path.Join(stateDir, "overlay"))
	if err != nil {
		return nil, err
	}
	return &OverlayManager{
		dockerHost: dockerHost,

		templatesDir: path.Join(templatesDir, "overlay"),
		stateDir:     absStateDir,
		serviceDir:   serviceDir,

		registryURL:    registryURL,
		registryCA:     registryCA,
		serviceAddress: serviceAddress,

		network: network,
		subnet:  subnet,

		tenants: make(map[string]*tenantOverlay),

		uid: defaultUID,
		gid: defaultGID,
	}, nil
}

func (o *OverlayManager) Reserve(jobID, tenant, tenantToken string, overlay *JobOverlay) (*OverlayContext, error) {
	if overlay == nil {
		return nil, nil
	}

	// only SecureSDN overlays support (for now)
	overlayType := overlay.Type
	if overlayType != "ssdn" {
		return nil, fmt.Errorf("unsupported overlay network type: %s", overlayType)
	}

	network, err := parseNetworkDefault(overlay.Network, o.network)
	if err != nil {
		return nil, err
	}
	if network == nil {
		return nil, fmt.Errorf("overlay configuration missing network")
	}
	subnet, err := parseNetworkDefault(overlay.Subnet, o.subnet)
	if err != nil {
		return nil, err
	}
	if subnet == nil {
		return nil, fmt.Errorf("overlay configuration missing network")
	}

	o.mutex.Lock()
	defer o.mutex.Unlock()

	to, err := o.getTenantOverlay(tenant, network, subnet)
	if err != nil {
		return nil, err
	}

	ip, err := to.reserve(jobID, network, subnet)
	if err != nil {
		return nil, err
	}

	return &OverlayContext{
		Type: overlayType,

		Network: network.String(),
		Subnet:  subnet.String(),

		IP: ip,
	}, nil
}

func (o *OverlayManager) Restore(oc *OverlayContext, jobID, tenant string) error {
	if oc == nil {
		return nil
	}

	network, err := parseNetwork(oc.Network)
	if err != nil {
		return err
	}
	subnet, err := parseNetwork(oc.Subnet)
	if err != nil {
		return err
	}

	o.mutex.Lock()
	defer o.mutex.Unlock()

	to, err := o.getTenantOverlay(tenant, network, subnet)
	if err != nil {
		return err
	}

	if err := to.restore(jobID, oc.IP, network, subnet); err != nil {
		return err
	}
	// mark overlay as initialized so it gets pruned when unused
	to.initialized = true
	return nil
}

func parseNetwork(networkString string) (*net.IPNet, error) {
	_, network, err := net.ParseCIDR(networkString)
	return network, err
}

func parseNetworkDefault(networkString string, defaultValue *net.IPNet) (*net.IPNet, error) {
	if len(networkString) > 0 {
		_, network, err := net.ParseCIDR(networkString)
		return network, err
	}
	return defaultValue, nil
}

func (o *OverlayManager) getTenantOverlay(tenant string, network, subnet *net.IPNet) (*tenantOverlay, error) {
	to, present := o.tenants[tenant]
	if present {
		return to, nil
	}

	to, err := newTenantOverlay(network, subnet)
	if err != nil {
		return nil, err
	}
	o.tenants[tenant] = to
	return to, nil
}

type OverlayAdvertisement struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

func (o *OverlayManager) Initialize(oc *OverlayContext, jobID, tenant string, tenantToken string, overlay *JobOverlay, c *containerConfig) error {
	if oc == nil {
		return nil
	}

	n := len(overlay.Services)
	ads := make([]OverlayAdvertisement, n)
	for i, v := range overlay.Services {
		ads[i].Name = v.Name
		ads[i].Location = fmt.Sprintf("tcp://%s:%d", oc.IP, v.Port)
	}
	servicesData, err := json.Marshal(ads)
	if err != nil {
		return err
	}

	o.mutex.Lock()
	defer o.mutex.Unlock()

	to, present := o.tenants[tenant]
	if !present {
		return fmt.Errorf("tenant %s has no overlays on this agent", tenant)
	}

	if !to.initialized {
		to.initialized = true
		if err := o.startOverlay(tenant, tenantToken, to, overlay); err != nil {
			return err
		}
	}

	labels := c.labels
	labels[OverlayTenantLabel] = tenant
	labels[OverlayIPLabel] = oc.IP
	labels[OverlayServicesLabel] = string(servicesData)

	c.overlayDNS = []string{to.dnsIP.String()}

	return nil
}

func (o *OverlayManager) Stopped(oc *OverlayContext, jobID, tenant string) error {
	if oc == nil {
		return nil
	}

	o.mutex.Lock()
	defer o.mutex.Unlock()

	to, present := o.tenants[tenant]
	if !present {
		return fmt.Errorf("tenant %s has no overlays on this agent", tenant)
	}

	unused, err := to.release(jobID)
	if err != nil {
		return nil
	}
	if !unused {
		return nil
	}

	delete(o.tenants, tenant)
	if !to.initialized {
		return nil
	}
	return o.stopOverlay(tenant)
}

func (o *OverlayManager) startOverlay(tenant, tenantToken string, to *tenantOverlay, overlay *JobOverlay) error {
	tenantID, err := comm.GenerateIdentifier(tenantIDLength)
	if err != nil {
		return err
	}

	log.Info("Starting overlay for tenant %s", tenant)

	overlayServiceDir := o.overlayServiceDir(tenant)
	watchServiceDir := o.watchServiceDir(tenant)
	dnsServiceDir := o.dnsServiceDir(tenant)

	ctx := map[string]interface{}{
		"dockerHost": o.dockerHost,

		"overlayServiceDir": overlayServiceDir,
		"watchServiceDir":   watchServiceDir,
		"dnsServiceDir":     dnsServiceDir,

		"outputDir": path.Join(watchServiceDir, "output"),

		"registryURL":    o.registryURL,
		"serviceAddress": o.serviceAddress,

		"tenant":      tenant,
		"tenantID":    tenantID,
		"tenantToken": tenantToken,

		"network": to.network.String(),
		"subnet":  to.subnet.String(),

		"gatewayIP": to.gatewayIP.String(),
		"dnsIP":     to.dnsIP.String(),
		"uid":       o.uid,
		"gid":       o.gid,
	}

	// TLS settings for overlay service
	overlayTLS := &tlsSettings{
		ca:       overlay.CA,
		cert:     overlay.Cert,
		key:      overlay.Key,
		peerName: overlay.PeerName,
	}

	// TLS settings for watch service
	watchTLS := &tlsSettings{
		ca: o.registryCA,
	}

	// render the service init scripts
	if err := o.createService(overlayServiceDir, "overlay-run", "overlay-clean", ctx, overlayTLS); err != nil {
		return err
	}
	if err := o.createService(watchServiceDir, "watch-run", "", ctx, watchTLS); err != nil {
		return err
	}
	if err := o.createService(dnsServiceDir, "dns-run", "", ctx, nil); err != nil {
		return err
	}

	return nil
}

func (o *OverlayManager) addTLSPEMFile(servicePath, name, contents string) (bool, error) {
	if len(contents) == 0 {
		return false, nil
	}
	if err := os.MkdirAll(servicePath, OverlayDirsMode); err != nil {
		return false, err
	}
	pemFile := path.Join(servicePath, name)
	if err := ioutil.WriteFile(pemFile, []byte(contents), OverlayTLSFileMode); err != nil {
		return false, err
	}
	return true, nil
}

func (o *OverlayManager) configureTLS(servicePath string, context map[string]interface{}, serviceTLS *tlsSettings) (map[string]interface{}, error) {
	// drop TLS config files in place
	hasCA, err := o.addTLSPEMFile(servicePath, "ca.pem", serviceTLS.ca)
	if err != nil {
		return nil, err
	}

	hasCert, err := o.addTLSPEMFile(servicePath, "cert.pem", serviceTLS.cert)
	if err != nil {
		return nil, err
	}

	hasKey, err := o.addTLSPEMFile(servicePath, "cert.key", serviceTLS.key)
	if err != nil {
		return nil, err
	}

	// copy the context before modifying it
	modified := make(map[string]interface{})
	for k, v := range context {
		modified[k] = v
	}

	// update the context based on TLS settings
	modified["ca"] = hasCA
	modified["cert"] = hasCert
	modified["key"] = hasKey
	if hasCA || hasCert || hasKey {
		modified["tls"] = true
	}
	modified["peerName"] = serviceTLS.peerName

	return modified, nil
}

func (o *OverlayManager) stopOverlay(tenant string) error {
	log.Info("Stopping overlay for tenant %s", tenant)

	var stopError error

	overlayServiceDir := o.overlayServiceDir(tenant)
	if err := o.stopService(overlayServiceDir); err != nil {
		log.Errorf("Failed to clean up overlay service: %s", err)
		stopError = err
	}
	if err := o.stopService(o.watchServiceDir(tenant)); err != nil {
		log.Errorf("Failed to clean up watch service: %s", err)
		stopError = err
	}
	if err := o.stopService(o.dnsServiceDir(tenant)); err != nil {
		log.Errorf("Failed to clean up DNS service: %s", err)
		stopError = err
	}

	return stopError
}

func (o *OverlayManager) overlayServiceDir(tenant string) string {
	return path.Join(o.stateDir, fmt.Sprintf("%s-ssdn", tenant))
}

func (o *OverlayManager) watchServiceDir(tenant string) string {
	return path.Join(o.stateDir, fmt.Sprintf("%s-watch", tenant))
}

func (o *OverlayManager) dnsServiceDir(tenant string) string {
	return path.Join(o.stateDir, fmt.Sprintf("%s-dns", tenant))
}

func (o *OverlayManager) createService(servicePath, script, clean string, context map[string]interface{}, serviceTLS *tlsSettings) error {
	// check if the service was already created
	exists, err := pathExists(servicePath)
	if err != nil {
		return err
	}
	if exists {
		log.Info("Service directory %s already exists; overwriting existing config", servicePath)
	}

	// update context with TLS config
	if serviceTLS != nil {
		modified, err := o.configureTLS(servicePath, context, serviceTLS)
		if err != nil {
			return err
		}
		context = modified
	}

	// render the run scripts
	runScript, err := o.render(script, context)
	if err != nil {
		return err
	}
	logRunScript, err := o.render("log-run", context)
	if err != nil {
		return err
	}

	// render the cleanup script
	cleanScript := ""
	if len(clean) > 0 {
		cleanScript, err = o.render(clean, context)
		if err != nil {
			return err
		}
	}

	// drop the scripts in place
	if err := os.MkdirAll(servicePath, OverlayDirsMode); err != nil {
		return err
	}
	runPath := path.Join(servicePath, "run")
	if err := ioutil.WriteFile(runPath, []byte(runScript), OverlayScriptMode); err != nil {
		return err
	}
	if len(cleanScript) > 0 {
		cleanPath := path.Join(servicePath, "clean")
		if err := ioutil.WriteFile(cleanPath, []byte(cleanScript), OverlayScriptMode); err != nil {
			return err
		}
	}

	logPath := path.Join(servicePath, "log")
	if err := os.MkdirAll(logPath, OverlayDirsMode); err != nil {
		return err
	}
	logRunPath := path.Join(logPath, "run")
	if err := ioutil.WriteFile(logRunPath, []byte(logRunScript), OverlayScriptMode); err != nil {
		return err
	}

	o.enableService(servicePath)
	return nil
}

func (o *OverlayManager) enableService(servicePath string) {
	// drop a symlink in the services directory
	if err := os.Symlink(servicePath, o.serviceLinkDirectory(servicePath)); err != nil {
		// can happen if symlink already exists
		log.Warn("Could not enable service at %s: %s", servicePath, err)
	}
}

func (o *OverlayManager) stopService(servicePath string) error {
	// stop the service
	if err := invoke("sv", "stop", servicePath); err != nil {
		// can happen if service failed to start
		log.Warn("Failed to stop service at %s: %s", servicePath, err)
	}

	// remove symlink in the services directory
	if err := os.Remove(o.serviceLinkDirectory(servicePath)); err != nil {
		return err
	}

	// run cleanup script, if any
	cleanPath := path.Join(servicePath, "clean")
	exists, err := pathExists(cleanPath)
	if err != nil {
		return err
	}
	if exists {
		if err := invoke(cleanPath); err != nil {
			return err
		}
	}

	return nil
}

func (o *OverlayManager) serviceLinkDirectory(servicePath string) string {
	serviceName := path.Base(servicePath)
	return path.Join(o.serviceDir, serviceName)
}

func (o *OverlayManager) render(templateName string, context interface{}) (string, error) {
	templateFile := path.Join(o.templatesDir, fmt.Sprintf("%s.mustache", templateName))
	template, err := ioutil.ReadFile(templateFile)
	if err != nil {
		return "", err
	}
	rendered := mustache.Render(string(template), context)
	if log.IsTraceEnabled() {
		log.Trace("Rendered template %s: %s", templateName, rendered)
	}
	return rendered, nil
}
