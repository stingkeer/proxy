function beforeRequest(req) {
  console.log('Processing request:', req.method, req.url);

  // Example: Add a custom header
  // req.headers['X-Custom-Header'] = 'MyValue';

  // Example: Modify request body for POST/PUT
  // if (req.method === 'POST' && req.body) {
  //   try {
  //     const data = JSON.parse(req.body);
  //     data.modifiedBy = 'proxy';
  //     req.body = JSON.stringify(data);
  //   } catch (e) {
  //     console.log('Failed to parse request body');
  //   }
  // }

  return req;
}

function handleResponse(req, resp) {
  console.log('Processing response:', resp.status, resp.url);
  console.log('Request method:', req.method);
  if (req.body) {
    console.log('Request body:', req.body);
  }

  // Example: Add a custom response header
  // resp.headers['X-Proxy-Version'] = '1.0';

  // Example: Modify JSON response body based on request parameters
  // if (resp.headers['Content-Type'] && resp.headers['Content-Type'].includes('application/json')) {
  //   try {
  //     const data = JSON.parse(resp.body);
  //     data.processedBy = 'proxy';
  //     data.requestMethod = req.method;
  //     resp.body = JSON.stringify(data);
  //   } catch (e) {
  //     console.log('Failed to parse response body');
  //   }
  // }

  // Example: Change response status
  // resp.status = 200;

  return resp;
}
