package overlay

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/catalyzeio/go-core/comm"
	"github.com/gorilla/mux"
)

type Listener struct {
	connector    Connector
	peerManager  PeerManager
	routeTracker *RouteTracker
	resolver     Resolver

	dsDir  string
	dsPath string

	start time.Time
}

func NewListener(tenant, runDir string) *Listener {
	dsDir := path.Join(runDir, tenant)
	return &Listener{
		dsDir:  dsDir,
		dsPath: path.Join(dsDir, "ssdn.sock"),

		start: time.Now(),
	}
}

func (l *Listener) Listen(connector Connector, peerManager PeerManager, routeTracker *RouteTracker, resolver Resolver) error {
	l.connector = connector
	l.peerManager = peerManager
	l.routeTracker = routeTracker
	l.resolver = resolver

	// set up domain socket listener
	if err := os.MkdirAll(l.dsDir, 0700); err != nil {
		return err
	}
	listener, err := comm.DomainSocketListener(l.dsPath)
	if err != nil {
		return err
	}

	// set up routes
	r := mux.NewRouter()

	r.HandleFunc("/status", l.status).Methods("GET")

	r.HandleFunc("/connections", l.connections).Methods("GET")
	r.HandleFunc("/connections/attach", l.attach).Methods("POST")
	r.HandleFunc("/connections/detach", l.detach).Methods("POST")

	// encoding peer URLs in paths causes issues; use explicit verb POST routes instead
	r.HandleFunc("/peers", l.peers).Methods("GET")
	r.HandleFunc("/peers/add", l.addPeer).Methods("POST")
	r.HandleFunc("/peers/delete", l.deletePeer).Methods("POST")

	r.HandleFunc("/routes", l.routes).Methods("GET")

	r.HandleFunc("/arp", l.arpTable).Methods("GET")
	r.HandleFunc("/arp/{ip}", l.resolve).Methods("GET")

	log.Info("Domain socket listening on %s", l.dsPath)

	return http.Serve(listener, r)
}

func (l *Listener) status(w http.ResponseWriter, r *http.Request) {
	uptime := time.Now().Sub(l.start)
	res := &Status{uptime.String()}
	if err := json.NewEncoder(w).Encode(res); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) connections(w http.ResponseWriter, r *http.Request) {
	if l.connector == nil {
		sendUnsupported(w)
		return
	}

	if err := json.NewEncoder(w).Encode(l.connector.ListConnections()); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) attach(w http.ResponseWriter, r *http.Request) {
	if l.connector == nil {
		sendUnsupported(w)
		return
	}

	data := &AttachRequest{}
	if err := json.NewDecoder(r.Body).Decode(data); err != nil {
		sendError(w, err)
		return
	}

	if err := l.connector.Attach(data.Container, data.IP); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) detach(w http.ResponseWriter, r *http.Request) {
	if l.connector == nil {
		sendUnsupported(w)
		return
	}

	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		sendError(w, err)
		return
	}

	if err := l.connector.Detach(string(data)); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) peers(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(l.peerManager.ListPeers()); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) addPeer(w http.ResponseWriter, r *http.Request) {
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		sendError(w, err)
		return
	}

	if err := l.peerManager.AddPeer(string(data)); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) deletePeer(w http.ResponseWriter, r *http.Request) {
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		sendError(w, err)
		return
	}

	if err := l.peerManager.DeletePeer(string(data)); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) routes(w http.ResponseWriter, r *http.Request) {
	if l.routeTracker == nil {
		sendUnsupported(w)
		return
	}

	var result []string
	routes := l.routeTracker.Get()
	result = make([]string, len(routes))
	for i, v := range routes {
		result[i] = v.String()
	}
	if err := json.NewEncoder(w).Encode(routes); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) arpTable(w http.ResponseWriter, r *http.Request) {
	if l.resolver == nil {
		sendUnsupported(w)
		return
	}

	result := l.resolver.ARPTable()
	if err := json.NewEncoder(w).Encode(result); err != nil {
		sendError(w, err)
	}
}

func (l *Listener) resolve(w http.ResponseWriter, r *http.Request) {
	if l.resolver == nil {
		sendUnsupported(w)
		return
	}

	vars := mux.Vars(r)
	ipString := vars["ip"]
	ip := net.ParseIP(ipString)
	if ip == nil {
		sendError(w, fmt.Errorf("invalid IP address: %s", ipString))
		return
	}

	resolved, err := l.resolver.Resolve(ip)
	if err != nil {
		sendError(w, err)
		return
	}
	if resolved == nil {
		http.NotFound(w, r)
		return
	}

	if _, err := fmt.Fprint(w, resolved); err != nil {
		sendError(w, err)
	}
}

func sendUnsupported(w http.ResponseWriter) {
	http.Error(w, "unsupported operation", http.StatusMethodNotAllowed)
}

func sendError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
