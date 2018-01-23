package scheduler

import (
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"

	"github.com/catalyzeio/go-core/comm"
	"github.com/gorilla/mux"

	"github.com/catalyzeio/paas-orchestration/agent"
)

const (
	DefaultPort = 7400
)

type Status struct {
	OK     bool    `json:"ok"`
	Alerts []Alert `json:"alerts"`

	QueueLength int `json:"queueLength"`
}

type Listener struct {
	address *comm.Address
	config  *tls.Config

	sched *Scheduler
}

func NewListener(address *comm.Address, config *tls.Config, sched *Scheduler) *Listener {
	return &Listener{address, config, sched}
}

func (l *Listener) Listen() error {
	r := mux.NewRouter()
	r.HandleFunc("/status", l.status).Methods("GET")
	r.HandleFunc("/jobs", l.jobs).Methods("GET", "POST")
	r.HandleFunc("/jobs/{jobID:.*}/companion", l.companion).Methods("POST")
	r.HandleFunc("/queue", l.queue).Methods("POST")
	r.HandleFunc("/jobs/{jobID:.*}", l.job).Methods("GET", "DELETE", "PATCH", "PUT")
	r.HandleFunc("/jobs/{jobID:.*}/stop", l.stop).Methods("POST")
	r.HandleFunc("/jobs/{jobID:.*}/start", l.start).Methods("POST")
	r.HandleFunc("/agents", l.agents).Methods("GET")
	r.HandleFunc("/usage", l.usage).Methods("GET")
	r.HandleFunc("/mode", l.mode).Methods("GET", "POST")
	r.HandleFunc("/agents-state", l.agentsState).Methods("GET")
	address := l.address
	listener, err := address.Listen(l.config)
	if err != nil {
		return err
	}

	mode := "http"
	if address.TLS() {
		mode = "https"
	}
	log.Info("Server listening on %s://%s:%d", mode, address.Host(), address.Port())

	return http.Serve(listener, r)
}

func (l *Listener) status(w http.ResponseWriter, r *http.Request) {
	var err error

	queueLength := l.sched.JobQueueLength()
	alerts := l.sched.Alerts()
	res := &Status{
		OK:     len(alerts) < 1,
		Alerts: alerts,

		QueueLength: queueLength,
	}
	err = json.NewEncoder(w).Encode(res)

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) jobs(w http.ResponseWriter, r *http.Request) {
	var err error
	switch r.Method {
	case "GET":
		res := l.sched.ListJobs()
		err = json.NewEncoder(w).Encode(res)
	case "POST":
		job := &agent.JobRequest{}
		if err = json.NewDecoder(r.Body).Decode(job); err == nil {
			var res *JobDetails
			if res, err = l.sched.LaunchJob(job); err == nil {
				err = json.NewEncoder(w).Encode(res)
			}
		}
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) companion(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var err error

	job := &agent.JobRequest{}
	if err = json.NewDecoder(r.Body).Decode(job); err == nil {
		var res *JobDetails
		if res, err = l.sched.LaunchCompanionJob(vars["jobID"], job); err == nil {
			err = json.NewEncoder(w).Encode(res)
		}
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) queue(w http.ResponseWriter, r *http.Request) {
	var err error

	job := &agent.JobRequest{}
	if err = json.NewDecoder(r.Body).Decode(job); err == nil {
		var res *JobDetails
		if res, err = l.sched.EnqueueJob(job); err == nil {
			err = json.NewEncoder(w).Encode(res)
		}
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) job(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var err error

	jobID := vars["jobID"]
	switch r.Method {
	case "GET":
		job, err := l.sched.ListJob(jobID)
		if err == nil {
			err = json.NewEncoder(w).Encode(job)
		}
	case "DELETE":
		err = l.sched.KillJob(jobID)
	case "PATCH":
		patch := &agent.JobPayload{}
		if err = json.NewDecoder(r.Body).Decode(patch); err == nil {
			err = l.sched.PatchJob(jobID, patch)
		}
	case "PUT":
		job := &agent.JobRequest{}
		if err = json.NewDecoder(r.Body).Decode(job); err == nil {
			var res *JobDetails
			if res, err = l.sched.ReplaceJob(jobID, job); err == nil {
				err = json.NewEncoder(w).Encode(res)
			}
		}
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) stop(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var err error

	jobID := vars["jobID"]
	err = l.sched.StopJob(jobID)

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) start(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	var err error

	jobID := vars["jobID"]
	err = l.sched.StartJob(jobID)

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) agents(w http.ResponseWriter, r *http.Request) {
	var err error

	res := l.sched.ListAgents()
	err = json.NewEncoder(w).Encode(res)

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) usage(w http.ResponseWriter, r *http.Request) {
	var err error

	var res map[string]*agent.PolicyUsage
	if res, err = l.sched.GetUsage(); err == nil {
		err = json.NewEncoder(w).Encode(res)
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) mode(w http.ResponseWriter, r *http.Request) {
	var err error

	switch r.Method {
	case "GET":
		var res map[string]string
		if res, err = l.sched.GetMode(); err == nil {
			err = json.NewEncoder(w).Encode(res)
		}
	case "POST":
		var data []byte
		if data, err = ioutil.ReadAll(r.Body); err == nil {
			err = l.sched.SetMode(string(data))
		}
	}

	if err != nil {
		sendError(w, err)
	}
}

func (l *Listener) agentsState(w http.ResponseWriter, r *http.Request) {
	var err error

	var res map[string]*agent.State
	if res, err = l.sched.GetAgentsState(); err == nil {
		err = json.NewEncoder(w).Encode(res)
	}

	if err != nil {
		sendError(w, err)
	}
}

func sendError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
