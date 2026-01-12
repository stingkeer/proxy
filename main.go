package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

var _ http.Handler = &Server{}

type Server struct{}

func (s *Server) serveWebSocket(rw http.ResponseWriter, r *http.Request) {
	targetURL := fmt.Sprintf("ws://%s%s", *target, r.RequestURI)

	// Connect to target WebSocket
	targetConn, _, err := websocket.DefaultDialer.Dial(targetURL, nil)
	if err != nil {
		log.Printf("WebSocket dial error: %v", err)
		http.Error(rw, "Unable to connect to backend", http.StatusBadGateway)
		return
	}
	defer targetConn.Close()

	// Upgrade client connection
	clientConn, err := upgrader.Upgrade(rw, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer clientConn.Close()

	// Proxy messages between client and target
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			msgType, msg, err := clientConn.ReadMessage()
			if err != nil {
				return
			}
			if err := targetConn.WriteMessage(msgType, msg); err != nil {
				return
			}
		}
	}()

	for {
		msgType, msg, err := targetConn.ReadMessage()
		if err != nil {
			break
		}
		if err := clientConn.WriteMessage(msgType, msg); err != nil {
			break
		}
	}

	<-done
}

var client *http.Client

func initClient() {
	if *socks != "" {
		dialer, err := proxy.SOCKS5("tcp", *socks, nil, proxy.Direct)
		if err != nil {
			log.Fatalf("Failed to create SOCKS5 dialer: %v", err)
		}
		transport := &http.Transport{
			Dial: dialer.Dial,
		}
		client = &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		}
	} else {
		client = &http.Client{
			Timeout: 5 * time.Second,
		}
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		fmt.Printf("%s is websocket", r.RequestURI)
		s.serveWebSocket(rw, r)
		return
	}

	target := fmt.Sprintf("%s/%s", *target, r.RequestURI)
	target = strings.TrimSuffix(target, "/")

	var reqBodyBytes []byte
	if r.Body != nil {
		reqBodyBytes, _ = io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewBuffer(reqBodyBytes))
	}
	req, _ := http.NewRequest(r.Method, target, bytes.NewBuffer(reqBodyBytes))

	if *debug {
		dump, err := httputil.DumpRequest(r, true)
		if err == nil {
			fmt.Printf("Request:\n%s\n", dump)
		}
	}

	for k, vs := range r.Header {
		for _, e := range vs {
			req.Header.Add(k, e)
		}
	}

	now := time.Now()

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("err %s", err)
		return
	}
	for k, vs := range resp.Header {
		for _, e := range vs {
			rw.Header().Add(k, e)
		}
	}
	// rw.Header().Add("access-control-allow-origin", "*")
	rw.WriteHeader(resp.StatusCode)

	var respBodyBytes []byte
	if resp.Body != nil {
		respBodyBytes, _ = io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))
	}
	io.Copy(rw, bytes.NewBuffer(respBodyBytes))

	if *debug {
		contentType := resp.Header.Get("Content-Type")
		if strings.Contains(contentType, "text/event-stream") || strings.Contains(contentType, "application/json") {
			resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))
			dump, err := httputil.DumpResponse(resp, true)
			if err == nil {
				fmt.Printf("Response:\n%s\n", dump)
			}
		}
	}
	fmt.Printf("target [%s] %d %s \n", time.Since(now), resp.StatusCode, r.RequestURI)
}

var (
	target = flag.String("target", "", "target host")
	addr   = flag.String("addr", ":8080", "listen addr")
	socks  = flag.String("socks", "", "SOCKS proxy address (e.g. 127.0.0.1:1080)")
	debug  = flag.Bool("debug", false, "enable debug mode (print request and response)")

	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
)

func main() {
	flag.Parse()
	if len(os.Args) <= 1 {
		flag.Usage()
		return
	}
	initClient()
	fmt.Printf("Listen %s", *addr)
	if err := http.ListenAndServe(*addr, &Server{}); err != nil {
		log.Fatal(err)
	}
}
