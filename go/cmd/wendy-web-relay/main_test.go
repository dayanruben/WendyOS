package main

import "testing"

func TestBrokerEndpoint(t *testing.T) {
	for _, endpoint := range []string{"relay.dev.wendy.sh:443", "eu.relay.wendy.sh:443", "https://wendy-cloud-dev-tunnel-broker-nkohwk7hda-uc.a.run.app"} {
		if _, err := brokerEndpoint(endpoint); err != nil {
			t.Fatalf("reject valid broker %q: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"localhost:443", "127.0.0.1:443", "relay.wendy.sh:22", "relay.wendy.sh.attacker.test:443", "user@relay.wendy.sh:443", "relay.wendy.sh:443/path", "relay.wendy.sh:443?target=localhost", "relay.wendy.sh:443#fragment"} {
		if _, err := brokerEndpoint(endpoint); err == nil {
			t.Errorf("accepted forbidden destination %q", endpoint)
		}
	}
}
