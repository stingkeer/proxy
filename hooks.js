console.log('hooks.js loaded');

function handleRequest(req) {
  console.log('===== REQUEST =====');
  console.log('Method:', req.method);
  console.log('URL:', req.url);
  console.log('Headers:', JSON.stringify(req.headers, null, 2));
  console.log('Body length:', req.body ? req.body.length : 0);
  if (req.body) {
    console.log('Body:', req.body);
  }
  return req;
}

function handleResponse(resp) {
  console.log('===== RESPONSE =====');
  console.log('Status:', resp.status);
  console.log('Content-Type:', resp.headers['Content-Type'] || resp.headers['content-type']);
  console.log('Body length:', resp.body ? resp.body.length : 0);
  
  try {
    var data = JSON.parse(resp.body);
    data._proxy = { processed: true, timestamp: new Date().toISOString() };
    return JSON.stringify(data);
  } catch (e) {
    return resp.body;
  }
}

console.log('hooks.js functions defined');
