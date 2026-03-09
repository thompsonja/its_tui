const http = require('http');

const PORT = process.env.PORT || 4123;
const API_BASE = process.env.API_URL || 'http://localhost:9001';

const server = http.createServer((req, res) => {
  if (req.url !== '/') {
    res.writeHead(404);
    res.end('not found');
    return;
  }

  http.get(`${API_BASE}/hello`, (apiRes) => {
    let data = '';
    apiRes.on('data', (chunk) => { data += chunk; });
    apiRes.on('end', () => {
      let message = '(no response)';
      try {
        const parsed = JSON.parse(data);
        message = parsed.message || message;
      } catch (_) {}

      res.writeHead(200, { 'Content-Type': 'text/html' });
      res.end(`<!DOCTYPE html>
<html>
<head><title>Hello MFE</title></head>
<body>
  <h1>${message}</h1>
</body>
</html>\n`);
    });
  }).on('error', (err) => {
    res.writeHead(502, { 'Content-Type': 'text/plain' });
    res.end(`error reaching API: ${err.message}\n`);
  });
});

server.listen(PORT, () => {
  console.log(`MFE running at http://localhost:${PORT}`);
  console.log(`API URL: ${API_BASE}`);
});
