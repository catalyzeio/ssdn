package scheduler

import (
	"sync"
	"time"
)

const (
	alertCleanInterval = 1 * time.Minute
	alertTTL           = 1 * time.Hour
)

type Alert struct {
	Timestamp int64  `json:"timestamp"`
	Type      string `json:"type"`
	Message   string `json:"message"`
}

func NewAlert(errType string, err error) *Alert {
	return &Alert{time.Now().Unix(), errType, err.Error()}
}

type Alerts struct {
	mutex  sync.RWMutex
	alerts map[int]*Alert
	i      int
}

func NewAlerts() *Alerts {
	a := &Alerts{
		alerts: make(map[int]*Alert),
	}
	go a.maintenance()
	return a
}

func (a *Alerts) Add(errType string, err error) {
	alert := NewAlert(errType, err)

	a.mutex.Lock()
	defer a.mutex.Unlock()

	a.alerts[a.i] = alert
	a.i++
}

func (a *Alerts) Get() []Alert {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	var res []Alert
	for _, alert := range a.alerts {
		res = append(res, *alert)
	}
	return res
}

func (a *Alerts) maintenance() {
	clean := time.NewTicker(alertCleanInterval)
	defer clean.Stop()
	for {
		a.clean()
		<-clean.C
	}
}

func (a *Alerts) clean() {
	a.mutex.Lock()
	defer a.mutex.Unlock()

	allowance := time.Now().Add(-alertTTL).Unix()
	for i, alert := range a.alerts {
		if alert.Timestamp < allowance {
			delete(a.alerts, i)
		}
	}
}
