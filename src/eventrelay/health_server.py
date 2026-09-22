import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

READY = threading.Event()


def mark_ready():
    READY.set()


def mark_not_ready():
    READY.clear()


class HealthHandler(BaseHTTPRequestHandler):
    service_name = ""

    def do_GET(self):
        status = 200 if self.path == "/healthz" and READY.is_set() else 503 if self.path == "/healthz" else 404
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        if status == 200:
            self.wfile.write(
                json.dumps(
                    {
                        "status": "ok" if READY.is_set() else "starting",
                        "service": self.service_name,
                    }
                ).encode()
            )

    def log_message(self, *_):
        return


def start_health_server(service_name):
    HealthHandler.service_name = service_name
    server = ThreadingHTTPServer(("0.0.0.0", 8080), HealthHandler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server
