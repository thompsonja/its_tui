const http = require('http');

const PORT = process.env.PORT || 3000;

const server = http.createServer((req, res) => {
  res.writeHead(200, { 'Content-Type': 'text/html' });
  res.end('<h1>Hello from MFE!</h1>\n');
});

server.listen(PORT, () => {
  console.log(`MFE running at http://localhost:${PORT}`);
});
