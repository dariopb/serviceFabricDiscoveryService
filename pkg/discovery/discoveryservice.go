package discovery

import (
	"context"
	"crypto/tls"
	"math/rand"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

type DiscoveryService struct {
	httpPort int
	cfgChan  chan []byte

	ctx            context.Context
	cancel         context.CancelFunc
	clientChannels map[string]chan []byte
	mtx            sync.Mutex
}

// NewDiscoveryService creates a new discovery service on the http port using the passed cert
func NewDiscoveryService(config *Config, cert *tls.Certificate, httpPort int) (*DiscoveryService, error) {
	rand.Seed(time.Now().UnixNano())

	disco := &DiscoveryService{
		httpPort:       httpPort,
		cfgChan:        make(chan []byte),
		clientChannels: make(map[string]chan []byte),
	}

	disco.ctx, disco.cancel = context.WithCancel(context.Background())

	provider, err := NewDiscoveryWorker(disco.ctx, config, "discovery")
	if err != nil {
		return nil, err
	}

	err = provider.Init()
	if err != nil {
		return nil, err
	}

	err = provider.Provide(disco.cfgChan)
	if err != nil {
		return nil, err
	}

	go disco.runLoop(disco.ctx)

	return disco, nil
}

// Close closes the discovery service
func (disco *DiscoveryService) Close() {
	disco.cancel()
}

//Subscribe subscribes to route data
func (disco *DiscoveryService) Subscribe(clientName string) chan []byte {
	disco.mtx.Lock()
	defer disco.mtx.Unlock()

	log.Infof("Subscribing client: [%s]", clientName)
	clientChan := make(chan []byte)
	disco.clientChannels[clientName] = clientChan

	return clientChan
}

//Unsubscribe unsubscribes from route data
func (disco *DiscoveryService) Unsubscribe(clientName string) {
	disco.mtx.Lock()
	defer disco.mtx.Unlock()

	log.Infof("Unsubscribing client: [%s]", clientName)
	clientChan, ok := disco.clientChannels[clientName]
	if ok {
		close(clientChan)
		delete(disco.clientChannels, clientName)
	}
}

func (disco *DiscoveryService) runLoop(ctx context.Context) {
loop:
	for {
		select {
		case data := <-disco.cfgChan:
			log.Debugf("New snapshot arrived: [%s]", string(data))

			disco.mtx.Lock()
			for clientName, ch := range disco.clientChannels {
				log.Infof("Sending update to: [%s]", clientName)
				ch <- data
			}
			disco.mtx.Unlock()
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			break loop
		}
	}
}