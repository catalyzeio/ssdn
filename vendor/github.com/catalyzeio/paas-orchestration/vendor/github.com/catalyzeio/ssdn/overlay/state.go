package overlay

import (
	"encoding/json"
	"os"
	"path"
	"time"
)

const (
	DataFile = "overlay.json"

	saveInterval = 15 * time.Second
)

type Snapshot struct {
	Connections map[string]*ConnectionDetails `json:"connections"`
}

type State struct {
	stateFile string

	updates  chan *Snapshot
	snapshot *Snapshot
	unsaved  bool
}

func NewState(tenant, runDir string) *State {
	const updateChannelSize = 8
	return &State{
		stateFile: path.Join(runDir, tenant, DataFile),

		updates: make(chan *Snapshot),
	}
}

func (s *State) Load() (*Snapshot, error) {
	file, err := os.Open(s.stateFile)
	if os.IsNotExist(err) {
		log.Info("State file '%s' not found; assuming fresh start", s.stateFile)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var snapshot *Snapshot
	if err := json.NewDecoder(file).Decode(&snapshot); err != nil {
		return nil, err
	}

	return snapshot, nil
}

func (s *State) Start() {
	go s.persist()
}

func (s *State) Update(snapshot *Snapshot) {
	s.updates <- snapshot
}

func (s *State) persist() {
	save := time.NewTicker(saveInterval)
	defer save.Stop()
	for {
		select {
		case <-save.C:
			if s.unsaved {
				s.writeState()
			}
		case s.snapshot = <-s.updates:
			s.writeState()
		}
	}
}

func (s *State) writeState() {
	s.unsaved = true

	tempFile := s.stateFile + ".new"
	file, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Errorf("Failed to create new state file: %s", err)
		return
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(s.snapshot); err != nil {
		log.Errorf("Failed to serialize state file: %s", err)
		return
	}
	if log.IsDebugEnabled() {
		log.Debug("Wrote state file to %s", tempFile)
	}

	if err := os.Rename(tempFile, s.stateFile); err != nil {
		log.Errorf("Failed to rename state file: %s", err)
	}

	s.unsaved = false
	log.Info("Updated state file %s", s.stateFile)
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	return true, err
}
