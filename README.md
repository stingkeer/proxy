# QuickJS Proxy

A simple HTTP proxy server with JavaScript-based request/response modification support.

## Features

- HTTP/HTTPS proxy
- WebSocket support
- SOCKS5 proxy support
- **JavaScript-based request/response hooks** (powered by QuickJS)
- Debug mode for request/response logging

## Installation

```bash
go build -o proxy
```

## Usage

### Basic Proxy

```bash
./proxy -target http://example.com -addr :8080
```

### With JavaScript Hooks

```bash
./proxy -target http://example.com -addr :8080 -script hooks.js
```

### With SOCKS5 Proxy

```bash
./proxy -target http://example.com -addr :8080 -socks 127.0.0.1:1080
```

### Debug Mode

```bash
./proxy -target http://example.com -addr :8080 -debug
```

## JavaScript API

Your JavaScript file should export two optional functions:

### `handleRequest(req)`

Called before the request is forwarded to the target.

**Parameters:**
- `req.method` (string): HTTP method (GET, POST, etc.)
- `req.url` (string): Request URL
- `req.headers` (object): Request headers
- `req.body` (string): Request body

**Returns:** Modified request object or null/undefined

### `handleResponse(resp)`

Called before the response is sent back to the client.

**Parameters:**
- `resp.status` (number): HTTP status code
- `resp.headers` (object): Response headers
- `resp.body` (string): Response body
- `resp.url` (string): Request URL

**Returns:** Modified response object or null/undefined

## Example: `hooks.js`

```javascript
function handleRequest(req) {
  console.log('[REQUEST]', req.method, req.url);
  
  // Add custom header
  if (!req.headers) {
    req.headers = {};
  }
  req.headers['X-Custom-Header'] = 'Hello from proxy';
  
  // Modify JSON body
  const contentType = req.headers['Content-Type'] || req.headers['content-type'];
  if (req.method === 'POST' && req.body && contentType && contentType.includes('application/json')) {
    try {
      const data = JSON.parse(req.body);
      data.timestamp = new Date().toISOString();
      req.body = JSON.stringify(data);
    } catch (e) {
      console.log('Failed to parse JSON');
    }
  }
  
  return req;
}

function handleResponse(resp) {
  console.log('[RESPONSE]', resp.status, resp.url);
  
  // Add CORS headers
  if (!resp.headers) {
    resp.headers = {};
  }
  resp.headers['Access-Control-Allow-Origin'] = '*';
  
  // Modify JSON response
  const contentType = resp.headers['Content-Type'] || resp.headers['content-type'];
  if (resp.body && contentType && contentType.includes('application/json')) {
    try {
      const data = JSON.parse(resp.body);
      if (typeof data === 'object') {
        data._proxy = { processed: true };
        resp.body = JSON.stringify(data);
      }
    } catch (e) {
      console.log('Failed to parse JSON');
    }
  }
  
  return resp;
}
```

## Flags

- `-target`: Target host (required)
- `-addr`: Listen address (default: `:8080`)
- `-socks`: SOCKS5 proxy address (e.g., `127.0.0.1:1080`)
- `-debug`: Enable debug mode (print request and response)
- `-script`: Path to JavaScript file for request/response modification

## Requirements

- Go 1.21+
