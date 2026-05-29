#!/usr/bin/env python3
"""Slow HTTP server for testing dashboard visibility of connections."""

import http.server
import socketserver
import time
import argparse

class SlowHandler(http.server.BaseHTTPRequestHandler):
    """Handler that responds slowly to keep connections alive."""
    
    def do_GET(self):
        self.send_response(200)
        self.send_header('Content-type', 'text/html')
        self.end_headers()
        
        # Send response slowly to keep connection alive
        lines = int(self.server.delay) // 1000
        for i in range(lines):
            self.wfile.write(f'<p>Line {i+1} - Connection active for {(i+1)} seconds</p>\n'.encode())
            self.wfile.flush()
            time.sleep(1)
        
        self.wfile.write(b'<p>Complete</p>')
    
    def log_message(self, format, *args):
        """Suppress log messages."""
        pass

def run_server(port, delay):
    """Run the slow HTTP server."""
    class SlowServer(http.server.HTTPServer):
        """Custom server that stores delay."""
        delay = delay
    
    with socketserver.TCPServer(("", port), SlowHandler) as httpd:
        httpd.delay = delay
        print(f"Slow HTTP server listening on port {port} with {delay}ms delay")
        print("Press Ctrl+C to stop")
        try:
            httpd.serve_forever()
        except KeyboardInterrupt:
            print("\nServer stopped")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Slow HTTP server for testing")
    parser.add_argument("--port", type=int, default=8766, help="Port to listen on")
    parser.add_argument("--delay", type=int, default=3000, help="Response delay in milliseconds")
    args = parser.parse_args()
    run_server(args.port, args.delay)
