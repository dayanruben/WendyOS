// wendy-web-relay forwards binary WebSocket bytes to one fixed agent endpoint.
// Agent mTLS is end-to-end; this process never receives operator private keys.
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/wendylabsinc/wendy/go/internal/shared/cloudrelay"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8788", "listen address; use a TLS reverse proxy for remote access")
	target := flag.String("target", "", "fixed agent mTLS host:port")
	origin := flag.String("origin", "http://localhost:5173", "exact allowed browser origin")
	cloud := flag.Bool("cloud", false, "enable the fixed api.dev.wendy.sh TLS relay at /cloud")
	flag.Parse()
	if _, _, err := net.SplitHostPort(*target); err != nil && !(*cloud && *target == "") {
		log.Fatal("-target must be an agent host:port")
	}
	u, err := url.Parse(*origin)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		log.Fatal("-origin must be a browser HTTP(S) origin")
	}
	mux := http.NewServeMux()
	handler := func(destination string, cloudTLS bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Origin") != *origin {
				http.Error(w, "Origin not allowed", http.StatusForbidden)
				return
			}
			ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{u.Host}})
			if err != nil {
				return
			}
			defer ws.CloseNow()
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			var tcp net.Conn
			if cloudTLS {
				tcp, err = (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: &tls.Config{ServerName: strings.Split(destination, ":")[0], MinVersion: tls.VersionTLS12, NextProtos: []string{"h2"}}}).DialContext(ctx, "tcp", destination)
			} else {
				tcp, err = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", destination)
			}
			if err != nil {
				ws.Close(websocket.StatusTryAgainLater, "Agent unavailable")
				return
			}
			defer tcp.Close()
			conn := websocket.NetConn(ctx, ws, websocket.MessageBinary)
			done := make(chan struct{})
			go func() { _, _ = io.Copy(tcp, conn); tcp.Close(); close(done) }()
			_, _ = io.Copy(conn, tcp)
			cancel()
			ws.CloseNow()
			<-done
		}
	}
	if *target != "" {
		mux.HandleFunc("GET /tunnel", handler(*target, false))
	}
	if *cloud {
		mux.HandleFunc("GET /cloud", handler("api.dev.wendy.sh:443", true))
		mux.HandleFunc("GET /broker", func(w http.ResponseWriter, r *http.Request) {
			endpoint, err := brokerEndpoint(r.URL.Query().Get("endpoint"))
			if err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}

			handler(endpoint, true)(w, r)
		})
	}
	log.Printf("Relay listening at %s; allowed origin %s", *listen, *origin)
	log.Fatal((&http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}

func brokerEndpoint(endpoint string) (string, error) {
	return cloudrelay.BrowserBrokerTarget(endpoint)
}
