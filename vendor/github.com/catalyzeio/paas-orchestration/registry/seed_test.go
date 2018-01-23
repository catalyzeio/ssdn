package registry

import (
	"flag"
	"testing"

	"github.com/catalyzeio/go-core/comm"
)

func TestSeedingRegistry(t *testing.T) {
	flag.Parse()
	registryListenAddress, err := comm.ParseAddress("tcp://127.0.0.1:8411")
	if err != nil {
		t.Fatal(err)
	}
	enum := map[string]*Enumeration{
		"tenant1": &Enumeration{
			Provides: map[string][]WeightedLocation{
				"code1": []WeightedLocation{WeightedLocation{"tcp://127.0.0.1:7444", 1.0, true}},
				"code2": []WeightedLocation{WeightedLocation{"tcp://127.0.0.1:7445", 0.5, true}, WeightedLocation{"tcp://127.0.0.1:7446", 0.5, true}},
			},
			Publishes: map[string][]WeightedLocation{},
		},
	}
	be := NewMemoryBackend(&enum)
	go NewListener(registryListenAddress, nil, be).Listen()
	registryClient, err := ClientFromURL("tenant1", nil, "tcp://127.0.0.1:8411")
	if err != nil {
		t.Fatal(err)
	}
	registryClient.Start(nil, true)
	err = registryClient.Advertise([]Advertisement{Advertisement{"code1", "tcp://127.0.0.1:7447", true}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := registryClient.Enumerate()
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Provides["code1"]) != 2 || len(e.Provides["code2"]) != 2 {
		t.Fatalf("Seeded data not found in registry")
	}
	err = be.RemoveSeed()
	if err != nil {
		t.Fatal(err)
	}
	e, err = registryClient.Enumerate()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Provides["code2"]; ok && len(e.Provides["code2"]) > 0 || len(e.Provides["code1"]) != 1 {
		t.Fatalf("Seeded data remained in registry even after it was revoked")
	}
}
