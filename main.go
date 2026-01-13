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
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Gaurav-Gosain/quickjs"
	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

var (
	rt           *quickjs.Runtime
	jsCtx        *quickjs.Context
	jsFile       string
	jsFileLoaded bool
	jsFileMutex  sync.RWMutex
)

func runRequestHook(r *http.Request, body *[]byte) error {
	jsFileMutex.RLock()
	ctx := jsCtx
	jsFileMutex.RUnlock()

	if ctx == nil {
		return nil
	}

	reqObj := ctx.Object()
	reqObj.Set("method", ctx.String(r.Method))
	reqObj.Set("url", ctx.String(r.URL.String()))
	reqObj.Set("headers", headersToObject(ctx, r.Header))
	reqObj.Set("body", ctx.String(string(*body)))

	hookFn, err := ctx.GetGlobal("handleRequest")
	if err != nil {
		return nil
	}

	if !hookFn.IsFunction() {
		return nil
	}

	result, err := hookFn.Call(ctx.Undefined(), reqObj)
	if err != nil {
		log.Printf("hookFn.Call error: %v", err)
		return err
	}

	if result.IsObject() {
		if newBody, err := result.Get("body"); err == nil {
			if newBody.IsString() {
				*body = []byte(newBody.String())
			}
		}
		if newHeaders, err := result.Get("headers"); err == nil && newHeaders.IsObject() {
			for k := range r.Header {
				r.Header.Del(k)
			}
			headerKeys := []string{"Content-Type", "Content-Length", "Authorization", "User-Agent", "Accept", "X-Custom-Header", "Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"}
			for _, key := range headerKeys {
				val, err := newHeaders.Get(key)
				if err == nil && val.IsString() {
					r.Header.Add(key, val.String())
				}
			}
		}
	} else {
		log.Printf("Result is not object, type: %v", result.IsObject())
	}

	return nil
}

func runResponseHook(r *http.Request, resp *http.Response, body *[]byte) error {
	jsFileMutex.RLock()
	ctx := jsCtx
	jsFileMutex.RUnlock()

	if ctx == nil {
		return nil
	}

	respObj := ctx.Object()
	respObj.Set("status", ctx.Int32(int32(resp.StatusCode)))
	respObj.Set("headers", headersToObject(ctx, resp.Header))
	respObj.Set("body", ctx.String(string(*body)))
	respObj.Set("url", ctx.String(r.URL.String()))

	hookFn, err := ctx.GetGlobal("handleResponse")
	if err != nil {
		return nil
	}

	if !hookFn.IsFunction() {
		return nil
	}

	result, err := hookFn.Call(ctx.Undefined(), respObj)
	if err != nil {
		log.Printf("Response hook call error: %v", err)
		return err
	}

	if result.IsUndefined() {
		log.Printf("Response hook returned undefined")
		return nil
	}

	if result.IsNull() {
		log.Printf("Response hook returned null")
		return nil
	}

	if result.IsString() {
		*body = []byte(result.String())
		return nil
	}

	if result.IsObject() {
		log.Printf("Response hook returned object")
		if newStatus, err := result.Get("status"); err == nil {
			if newStatus.IsNumber() {
				if status, err := newStatus.Int32(); err == nil {
					resp.StatusCode = int(status)
				}
			}
		}
		if newBody, err := result.Get("body"); err == nil {
			if newBody.IsString() {
				log.Printf("New response body length: %d", len(newBody.String()))
				*body = []byte(newBody.String())
			}
		}
		if newHeaders, err := result.Get("headers"); err == nil && newHeaders.IsObject() {
			log.Printf("Updating response headers")
			for k := range resp.Header {
				resp.Header.Del(k)
			}
			headerKeys := []string{"Content-Type", "Content-Length", "Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers"}
			for _, key := range headerKeys {
				val, err := newHeaders.Get(key)
				if err == nil && val.IsString() {
					resp.Header.Add(key, val.String())
				}
			}
		}
	}

	return nil
}

func headersToObject(ctx *quickjs.Context, headers http.Header) quickjs.Value {
	obj := ctx.Object()
	for k, vs := range headers {
		if len(vs) == 1 {
			obj.Set(k, ctx.String(vs[0]))
		} else {
			arr := ctx.Array()
			for i, v := range vs {
				arr.SetIdx(i, ctx.String(v))
			}
			obj.Set(k, arr)
		}
	}
	return obj
}

func loadJSFile() error {
	if jsFile == "" {
		return nil
	}

	if rt != nil {
		rt.Close()
		jsCtx = nil
	}

	var err error
	rt, err = quickjs.NewRuntime()
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	jsCtx, err = rt.NewContext()
	if err != nil {
		return fmt.Errorf("failed to create context: %w", err)
	}

	logFn := jsCtx.Function("log", func(ctx *quickjs.Context, this quickjs.Value, args []quickjs.Value) quickjs.Value {
		msg := ""
		for _, arg := range args {
			msg += arg.String() + " "
		}
		log.Printf("[JS] %s", strings.TrimSpace(msg))
		return jsCtx.Undefined()
	})
	global, _ := jsCtx.Global()
	global.Set("console", jsCtx.Object())
	console, _ := global.Get("console")
	console.Set("log", logFn)
	console.Set("error", logFn)

	content, err := os.ReadFile(jsFile)
	if err != nil {
		return fmt.Errorf("failed to read JS file: %w", err)
	}

	log.Printf("Loading JavaScript file: %s", jsFile)
	if _, err := jsCtx.Eval(string(content)); err != nil {
		return fmt.Errorf("failed to evaluate JS: %w", err)
	}

	jsFileMutex.Lock()
	jsFileLoaded = true
	jsFileMutex.Unlock()

	return nil
}

func watchJSFile() {
	log.Printf("Starting file watcher...")
	if jsFile == "" {
		log.Printf("No JS file specified, skipping file watcher")
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	absPath, err := filepath.Abs(jsFile)
	if err != nil {
		log.Printf("Failed to get absolute path: %v", err)
		return
	}

	dir := filepath.Dir(absPath)
	if err := watcher.Add(dir); err != nil {
		log.Printf("Failed to watch directory: %v", err)
		return
	}

	log.Printf("Watching JavaScript file for changes: %s", absPath)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			absEventPath, err := filepath.Abs(event.Name)
			if err != nil {
				continue
			}

			if absEventPath == absPath && (event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {
				log.Printf("JavaScript file modified, reloading...")
				if err := loadJSFile(); err != nil {
					log.Printf("Failed to reload JS file: %v", err)
				} else {
					log.Printf("JavaScript file reloaded successfully")
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

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

	jsFileMutex.RLock()
	shouldRunJS := jsFileLoaded && rt != nil
	jsFileMutex.RUnlock()

	if shouldRunJS {
		if err := runRequestHook(r, &reqBodyBytes); err != nil {
			log.Printf("Request hook error: %v", err)
		}
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
	rw.WriteHeader(resp.StatusCode)

	var respBodyBytes []byte
	if resp.Body != nil {
		respBodyBytes, _ = io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))
	}

	jsFileMutex.RLock()
	shouldRunJS = jsFileLoaded && rt != nil
	jsFileMutex.RUnlock()

	if shouldRunJS {
		if err := runResponseHook(r, resp, &respBodyBytes); err != nil {
			log.Printf("Response hook error: %v", err)
		}
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
	script = flag.String("script", "", "path to JavaScript file for request/response modification")

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

	jsFile = *script
	if jsFile != "" {
		if err := loadJSFile(); err != nil {
			log.Fatalf("Failed to load JS file: %v", err)
		}
		fmt.Printf("Loaded JavaScript script: %s\n", jsFile)

		go watchJSFile()
	}

	initClient()
	fmt.Printf("Listen %s", *addr)
	if err := http.ListenAndServe(*addr, &Server{}); err != nil {
		log.Fatal(err)
	}
}
