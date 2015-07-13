package overlay

import (
	"encoding/json"
	"io/ioutil"
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

func (l *Listener) Listen(connector Connector, peerManager PeerManager, routeTracker *RouteTracker) error {
	l.connector = connector
	l.peerManager = peerManager
	l.routeTracker = routeTracker

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
	r.HandleFunc("/connections", l.connections).Methods("GET", "POST")
	r.HandleFunc("/connections/{id:.*}", l.detach).Methods("DELETE")
	r.HandleFunc("/peers", l.peers).Methods("GET", "POST")
	r.HandleFunc("/peers/{id:.*}", l.deletePeer).Methods("DELETE")
	r.HandleFunc("/routes", l.routes).Methods("GET")

	log.Info("Domain socket listening on %s", l.dsPath)

	return http.Serve(listener, r)
}

func (l *Listener) status(w http.ResponseWriter, r *http.Request) {
	var err error

	uptime := time.Now().Sub(l.start)
	res := &Status{uptime.String()}
	err = json.NewEncoder(w).Encode(res)

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) connections(w http.ResponseWriter, r *http.Request) {
	var err error

	switch r.Method {
	case "GET":
		err = json.NewEncoder(w).Encode(l.connector.ListConnections())
	case "POST":
		data := &AttachRequest{}
		if err = json.NewDecoder(r.Body).Decode(data); err == nil {
			err = l.connector.Attach(data.Container)
		}
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) detach(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var err error

	err = l.connector.Detach(vars["id"])

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) peers(w http.ResponseWriter, r *http.Request) {
	var err error

	switch r.Method {
	case "GET":
		err = json.NewEncoder(w).Encode(l.peerManager.ListPeers())
	case "POST":
		var data []byte
		if data, err = ioutil.ReadAll(r.Body); err == nil {
			err = l.peerManager.AddPeer(string(data))
		}
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) deletePeer(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var err error

	err = l.peerManager.DeletePeer(vars["id"])

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) routes(w http.ResponseWriter, r *http.Request) {
	var err error

	var data []string
	if l.routeTracker != nil {
		routes := l.routeTracker.Get()
		data = make([]string, len(routes))
		for i, v := range routes {
			data[i] = v.String()
		}
	}
	err = json.NewEncoder(w).Encode(data)

	if err != nil {
		sendError(w, err)
	}
}

func sendError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
