package agent

import (
	"fmt"
)

// Agent-level job placement constraints.
type AgentConstraints struct {
	// hard constraints
	Requires  StringBag `json:"requires,omitempty"`
	Provides  StringBag `json:"provides,omitempty"`
	Conflicts StringBag `json:"conflicts,omitempty"`
	// soft constraints
	Prefers  StringBag `json:"prefers,omitempty"`
	Despises StringBag `json:"despises,omitempty"`
}

func (ac *AgentConstraints) Permitted(requires StringBag, provides StringBag, conflicts StringBag) bool {
	// verify agent requirements are met by inbound job
	if missing := ac.Requires.Difference(provides); len(missing) > 0 {
		if log.IsDebugEnabled() {
			log.Debug("Agent requirements not met by inbound job: %s", missing)
		}
		return false
	}

	// verify inbound job requirements are met by agent
	if missing := requires.Difference(ac.Provides); len(missing) > 0 {
		if log.IsDebugEnabled() {
			log.Debug("Inbound job requirements not met by agent: %s", missing)
		}
		return false
	}

	// verify inbound job does not conflict with agent requirements
	if forbidden := ac.Conflicts.Intersection(provides); len(forbidden) > 0 {
		if log.IsDebugEnabled() {
			log.Debug("Inbound job conflicts with agent services: %s", forbidden)
		}
		return false
	}

	// verify agent requirements do not conflict with inbound job
	if forbidden := conflicts.Intersection(ac.Provides); len(forbidden) > 0 {
		if log.IsDebugEnabled() {
			log.Debug("Agent services conflict with inbound job: %s", forbidden)
		}
		return false
	}

	return true
}

func (ac *AgentConstraints) Matches(services []string) int {
	return ac.Provides.Matches(services)
}

// Which services are currently present for this tenant.
type TenantServices struct {
	provides  StringBag
	conflicts StringBag
}

func NewTenantServices() *TenantServices {
	return &TenantServices{make(StringBag), make(StringBag)}
}

func (s *TenantServices) AddProvides(services []string) {
	s.provides.AddAll(services)
}

func (s *TenantServices) RemoveProvides(services []string) {
	s.provides.RemoveAll(services)
}

func (s *TenantServices) AddConflicts(services []string) {
	s.conflicts.AddAll(services)
}

func (s *TenantServices) RemoveConflicts(services []string) {
	s.conflicts.RemoveAll(services)
}

func (s *TenantServices) Permitted(provides StringBag, conflicts StringBag, removedConstraints *JobDescription) bool {
	// check if the inbound job conflicts with an existing job
	tsConflicts := s.conflicts
	tsProvides := s.provides
	if removedConstraints != nil {
		tsConflicts = s.conflicts.NewCopy()
		tsProvides = s.provides.NewCopy()
		tsConflicts.RemoveAll(removedConstraints.Conflicts)
		tsProvides.RemoveAll(removedConstraints.Provides)
	}
	if forbidden := tsConflicts.Intersection(provides); len(forbidden) > 0 {
		if log.IsDebugEnabled() {
			log.Debug("Inbound job conflicts with existing job constraints: %s", forbidden)
		}
		return false
	}

	// check if an existing job conflicts with the inbound job
	if forbidden := tsProvides.Intersection(conflicts); len(forbidden) > 0 {
		if log.IsDebugEnabled() {
			log.Debug("Existing job conflicts with inbound job constraints: %s", forbidden)
		}
		return false
	}

	return true
}

func (s *TenantServices) Matches(services []string) int {
	return s.provides.Matches(services)
}

func (s *TenantServices) Empty() bool {
	return len(s.provides) == 0 && len(s.conflicts) == 0
}

func (s *TenantServices) String() string {
	return fmt.Sprintf("[provides=%v, conflicts=%v]", s.provides, s.conflicts)
}
