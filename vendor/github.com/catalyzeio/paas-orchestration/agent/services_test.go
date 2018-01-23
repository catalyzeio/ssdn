package agent

import (
	"testing"
)

func TestDuplicateServices(t *testing.T) {
	s := NewTenantServices()

	s.AddProvides([]string{"service1", "service2", "service3"})
	s.AddConflicts([]string{"service88"})
	if c := s.Matches([]string{"service1", "service4"}); c != 1 {
		t.Fail()
	}

	s.AddProvides([]string{"service1"})
	if c := s.Matches([]string{"service4", "service1"}); c != 2 {
		t.Fail()
	}

	s.RemoveProvides([]string{"service1"})
	if c := s.Matches([]string{"service1", "service4"}); c != 1 {
		t.Fail()
	}

	s.RemoveProvides([]string{"service1", "service2", "service3"})
	s.RemoveConflicts([]string{"service88"})
	if c := s.Matches([]string{"service4", "service1"}); c != 0 {
		t.Fail()
	}
}

func TestInboundConflict(t *testing.T) {
	s := NewTenantServices()

	s.AddProvides([]string{"service1", "service2", "service3"})
	s.AddConflicts([]string{"service88"})

	provides := NewStringBag([]string{"service88"})
	conflicts := NewStringBag([]string{"service99"})

	jd := &JobDescription{
		Provides:  []string{},
		Conflicts: []string{"service88"},
	}

	if s.Permitted(provides, nil, nil) {
		t.Fail()
	}
	if !s.Permitted(nil, conflicts, nil) {
		t.Fail()
	}
	if s.Permitted(provides, conflicts, nil) {
		t.Fail()
	}
	if !s.Permitted(provides, conflicts, jd) {
		t.Fail()
	}
}

func TestExistingConflict(t *testing.T) {
	s := NewTenantServices()

	s.AddProvides([]string{"service1", "service2", "service3"})
	s.AddConflicts([]string{"service88"})

	provides := NewStringBag([]string{"service1"})
	conflicts := NewStringBag([]string{"service1"})

	jd := &JobDescription{
		Provides:  []string{"service1"},
		Conflicts: []string{},
	}

	if !s.Permitted(provides, nil, nil) {
		t.Fail()
	}
	if s.Permitted(nil, conflicts, nil) {
		t.Fail()
	}
	if s.Permitted(provides, conflicts, nil) {
		t.Fail()
	}
	if !s.Permitted(provides, conflicts, jd) {
		t.Fail()
	}
}

func TestNoConflict(t *testing.T) {
	s := NewTenantServices()

	s.AddProvides([]string{"service1", "service2", "service3"})
	s.AddConflicts([]string{"service88"})

	provides := NewStringBag([]string{"service4"})
	conflicts := NewStringBag([]string{"service4"})

	jd := &JobDescription{
		Provides:  []string{},
		Conflicts: []string{},
	}

	if !s.Permitted(provides, nil, nil) {
		t.Fail()
	}
	if !s.Permitted(nil, conflicts, nil) {
		t.Fail()
	}
	if !s.Permitted(provides, conflicts, nil) {
		t.Fail()
	}
	if !s.Permitted(provides, conflicts, jd) {
		t.Fail()
	}
}

func TestAgentRequirements(t *testing.T) {
	requires := NewStringBag([]string{"host.foo"})
	provides := NewStringBag([]string{"job.bar"})
	conflicts := NewStringBag([]string{"host.baz"})

	// agent missing job requirement
	ac := AgentConstraints{
		Requires: NewStringBag([]string{"job.bar"}),
	}
	if ac.Permitted(requires, provides, conflicts) {
		t.Errorf("agent requirement test 1")
	}

	// job missing agent requirement
	ac = AgentConstraints{
		Requires: NewStringBag([]string{"job.foo"}),
		Provides: NewStringBag([]string{"host.foo"}),
	}
	if ac.Permitted(requires, provides, conflicts) {
		t.Errorf("agent requirement test 2")
	}

	// agent providing wrong job requirement
	ac = AgentConstraints{
		Requires: NewStringBag([]string{"job.bar"}),
		Provides: NewStringBag([]string{"host.bar"}),
	}
	if ac.Permitted(requires, provides, conflicts) {
		t.Errorf("agent requirement test 3")
	}

	// job missing agent requirement
	ac = AgentConstraints{
		Requires: NewStringBag([]string{"job.foo"}),
		Provides: NewStringBag([]string{"host.foo"}),
	}
	if ac.Permitted(requires, provides, conflicts) {
		t.Errorf("agent requirement test 4")
	}

	// match
	ac = AgentConstraints{
		Requires: NewStringBag([]string{"job.bar"}),
		Provides: NewStringBag([]string{"host.foo"}),
	}
	if !ac.Permitted(requires, provides, conflicts) {
		t.Errorf("agent requirement test 5")
	}

	// job conflicts with agent
	ac = AgentConstraints{
		Requires: NewStringBag([]string{"job.bar"}),
		Provides: NewStringBag([]string{"host.foo", "host.baz"}),
	}
	if ac.Permitted(requires, provides, conflicts) {
		t.Errorf("agent requirement test 6")
	}

	// agent conflicts with job
	ac = AgentConstraints{
		Provides:  NewStringBag([]string{"host.foo"}),
		Conflicts: NewStringBag([]string{"job.bar"}),
	}
	if ac.Permitted(requires, provides, conflicts) {
		t.Errorf("agent requirement test 7")
	}
}
