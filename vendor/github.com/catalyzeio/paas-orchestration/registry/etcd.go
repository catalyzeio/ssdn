package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/mvcc/mvccpb"
)

const (
	leaseTTL = 60 // seconds

	requestTimeout              = time.Second * 10
	leaseRenewalInterval        = time.Second * 50
	leaseRenewalFailureInterval = time.Second * 2
)

type EtcdBackend struct {
	mutex  sync.RWMutex
	serial uint

	client        *clientv3.Client
	leaseID       clientv3.LeaseID
	keepAliveChan chan struct{}
	listeners     map[string]*ChangeListeners
}

func NewEtcdBackend(endpoints []string) *EtcdBackend {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: requestTimeout,
	})
	if err != nil {
		log.Info("Error constructing ETCD client: %s", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	lease, err := clientv3.NewLease(client).Grant(ctx, leaseTTL)
	cancel()
	if err != nil {
		log.Info("Error retrieving ETCD lease: %s", err)
		client.Close()
		return nil
	}
	keepAliveChan := make(chan struct{})
	go keepAlive(ctx, client, lease.ID, keepAliveChan)
	return &EtcdBackend{
		serial: 1,

		client:        client,
		leaseID:       lease.ID,
		keepAliveChan: keepAliveChan,
		listeners:     make(map[string]*ChangeListeners),
	}
}

func keepAlive(ctx context.Context, client *clientv3.Client, leaseID clientv3.LeaseID, done chan struct{}) {
	ticker := time.NewTicker(leaseRenewalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			resp, err := client.KeepAliveOnce(ctx, leaseID)
			if err != nil {
				log.Info("Error keeping lease alive: %s\n", err)
				time.Sleep(leaseRenewalFailureInterval)
				continue
			}
			log.Debug("Lease extended - %d seconds TTL\n", resp.TTL)
		}
	}
}

func (e *EtcdBackend) Advertise(tenant string, req *Message, resp *Message) error {
	if log.IsDebugEnabled() {
		log.Debug("Advertising provides=%v publishes=%v for %s", req.Provides, req.Publishes, tenant)
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	serial := e.serial
	e.serial++
	req.serial = serial

	ops := []clientv3.Op{}
	for _, publish := range req.Publishes {
		data, err := json.Marshal(ServiceLocationDetails{Serial: serial, Running: publish.Running})
		if err != nil {
			log.Info("Failed to marshal object: %s", err)
			continue
		}
		ops = append(ops, clientv3.OpPut(fmt.Sprintf("%s/publishes/%s/%s", tenant, publish.Name, publish.Location), string(data), clientv3.WithLease(e.leaseID)))
	}
	for _, provides := range req.Provides {
		data, err := json.Marshal(ServiceLocationDetails{Serial: serial, Running: provides.Running})
		if err != nil {
			log.Info("Failed to marshal object: %s", err)
			continue
		}
		ops = append(ops, clientv3.OpPut(fmt.Sprintf("%s/provides/%s/%s", tenant, provides.Name, provides.Location), string(data), clientv3.WithLease(e.leaseID)))
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	txnResp, err := e.client.Txn(ctx).Then(ops...).Commit()
	cancel()
	if err != nil || !txnResp.Succeeded {
		return fmt.Errorf("failed to advertise provides=%v publishes=%v for %s: %s", req.Provides, req.Publishes, tenant, err)
	}

	e.notifyListeners(tenant)
	resp.Type = "advertised"
	return nil
}

func (e *EtcdBackend) Unadvertise(tenant string, ads *Message, notify bool) error {
	if log.IsDebugEnabled() {
		log.Debug("Unadvertising provides=%v publishes=%v for %s", ads.Provides, ads.Publishes, tenant)
	}

	for _, publish := range ads.Publishes {
		key := fmt.Sprintf("%s/publishes/%s/%s", tenant, publish.Name, publish.Location)

		data, err := json.Marshal(ServiceLocationDetails{Serial: ads.serial, Running: publish.Running})
		if err != nil {
			log.Info("Failed to marshal object: %s", err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		txnResp, err := e.client.Txn(ctx).If(
			clientv3.Compare(clientv3.Value(key), "=", string(data)),
		).Then(
			clientv3.OpDelete(key),
		).Commit()
		cancel()
		if err != nil {
			return fmt.Errorf("failed to unadvertise publishes=%v for %s: %s", publish, tenant, err)
		} else if !txnResp.Succeeded {
			log.Warn("Ignoring obsolete unadvertise request for %s", publish.Location)
		}
	}
	for _, provides := range ads.Provides {
		key := fmt.Sprintf("%s/provides/%s/%s", tenant, provides.Name, provides.Location)

		data, err := json.Marshal(ServiceLocationDetails{Serial: ads.serial, Running: provides.Running})
		if err != nil {
			log.Info("Failed to marshal object: %s", err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		txnResp, err := e.client.Txn(ctx).If(
			clientv3.Compare(clientv3.Value(key), "=", string(data)),
		).Then(
			clientv3.OpDelete(key),
		).Commit()
		cancel()
		if err != nil {
			return fmt.Errorf("failed to unadvertise provides=%v for %s: %s", provides, tenant, err)
		} else if !txnResp.Succeeded {
			log.Warn("Ignoring obsolete unadvertise request for %s", provides.Location)
		}
	}

	if notify {
		e.mutex.RLock()
		defer e.mutex.RUnlock()

		e.notifyListeners(tenant)
	}
	return nil
}

func (e *EtcdBackend) notifyListeners(tenant string) {
	listeners := e.listeners[tenant]
	if listeners != nil {
		listeners.Notify()
	}
}

func (e *EtcdBackend) Query(tenant string, req *Message, resp *Message) error {
	requires := req.Requires
	if len(requires) == 0 {
		resp.SetError("query missing required service")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	getResp, err := e.client.Get(ctx, fmt.Sprintf("%s/provides/%s", tenant, requires), clientv3.WithPrefix())
	cancel()
	if err != nil {
		return err
	}
	for _, kv := range getResp.Kvs {
		_, _, _, location, _, err := parseKV(kv)
		if err != nil {
			log.Warn("Error parsing KV pair: %s", err)
			continue
		}

		resp.Location = location
		break
	}
	resp.Type = "answer"
	return nil
}

func (e *EtcdBackend) QueryAll(tenant string, req *Message, resp *Message) error {
	requires := req.Requires
	if len(requires) == 0 {
		resp.SetError("query missing required service")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	getResp, err := e.client.Get(ctx, fmt.Sprintf("%s/provides/%s", tenant, requires), clientv3.WithPrefix())
	cancel()
	if err != nil {
		return err
	}
	locations := NewServiceLocations()
	for _, kv := range getResp.Kvs {
		_, _, _, location, sld, err := parseKV(kv)
		if err != nil {
			log.Warn("Error parsing KV pair: %s", err)
			continue
		}

		locations.Add(sld, location)
	}

	resp.Locations = locations.Locations()
	resp.Type = "answer"
	return nil
}

func (e *EtcdBackend) Enumerate(tenant string, req *Message, resp *Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	getResp, err := e.client.Get(ctx, fmt.Sprintf("%s/", tenant), clientv3.WithPrefix())
	cancel()
	if err != nil {
		return err
	}
	svcs := NewServices()
	for _, kv := range getResp.Kvs {
		_, adType, name, location, sld, err := parseKV(kv)
		if err != nil {
			log.Warn("Error parsing KV pair: %s", err)
			continue
		}

		if adType == "publishes" {
			if _, ok := svcs.publishes[name]; !ok {
				svcs.publishes[name] = NewServiceLocations()
			}
			svcs.publishes[name].Add(sld, location)
		} else if adType == "provides" {
			if _, ok := svcs.provides[name]; !ok {
				svcs.provides[name] = NewServiceLocations()
			}
			svcs.provides[name].Add(sld, location)
		}
	}
	resp.Registry = svcs.GetEnumeration()
	resp.Type = "answer"
	return nil
}

func (e *EtcdBackend) Register(tenant string, notifications chan<- interface{}, resp *Message) error {
	if log.IsDebugEnabled() {
		log.Debug("Registering listener for %s", tenant)
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	listeners := e.listeners[tenant]
	if listeners == nil {
		listeners = NewChangeListeners()
		e.listeners[tenant] = listeners
	}
	listeners.Add(notifications)
	resp.Type = "registered"
	return nil
}

func (e *EtcdBackend) Unregister(tenant string, notifications chan<- interface{}, resp *Message) error {
	if log.IsDebugEnabled() {
		log.Debug("Unregistering listener for %s", tenant)
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	listeners := e.listeners[tenant]
	if listeners != nil {
		listeners.Remove(notifications)
		if listeners.Empty() {
			delete(e.listeners, tenant)
		}
	}
	if resp != nil {
		resp.Type = "unregistered"
	}
	return nil
}

func (e *EtcdBackend) EnumerateAll() (map[string]*Enumeration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	getResp, err := e.client.Get(ctx, "", clientv3.WithPrefix())
	cancel()
	if err != nil {
		return nil, err
	}
	tenantSvcs := map[string]*Services{}
	for _, kv := range getResp.Kvs {
		tenant, adType, name, location, sld, err := parseKV(kv)
		if err != nil {
			log.Warn("Error parsing KV pair: %s", err)
			continue
		}

		if _, present := tenantSvcs[tenant]; !present {
			tenantSvcs[tenant] = NewServices()
		}

		if adType == "publishes" {
			if _, present := tenantSvcs[tenant].publishes[name]; !present {
				tenantSvcs[tenant].publishes[name] = NewServiceLocations()
			}
			tenantSvcs[tenant].publishes[name].Add(sld, location)
		} else if adType == "provides" {
			if _, present := tenantSvcs[tenant].provides[name]; !present {
				tenantSvcs[tenant].provides[name] = NewServiceLocations()
			}
			tenantSvcs[tenant].provides[name].Add(sld, location)
		}
	}
	enums := map[string]*Enumeration{}
	for tenant, s := range tenantSvcs {
		enums[tenant] = s.GetEnumeration()
	}
	return enums, nil
}

func parseKV(kv *mvccpb.KeyValue) (string, string, string, string, ServiceLocationDetails, error) {
	key := string(kv.Key)
	var sld ServiceLocationDetails
	err := json.Unmarshal(kv.Value, &sld)
	if err != nil {
		return "", "", "", "", sld, fmt.Errorf("invalid value encountered for key %s: %s", key, string(kv.Value))
	}
	keyParts := strings.SplitN(key, "/", 4)
	if len(keyParts) != 4 {
		return "", "", "", "", sld, fmt.Errorf("key is not of the form {tenant}/{type}/{name}/{location}: %s", key)
	}
	return keyParts[0], keyParts[1], keyParts[2], keyParts[3], sld, nil
}

func (e *EtcdBackend) RemoveSeed() error {
	// seed functionality is not applicable to etcd
	return nil
}

func (e *EtcdBackend) Close() {
	e.keepAliveChan <- struct{}{}
	e.client.Close()
}
