from SimpleHTTPServer import SimpleHTTPRequestHandler
from structlog import get_logger
from connection_mgr import ConnectionManager
log = get_logger()

class Probe(SimpleHTTPRequestHandler):

    def do_GET(self):

        if self.path == '/healthz':
            self.health_probe()

        elif self.path == '/ready':
            self.ready_probe()

    def health_probe(self):

        if ConnectionManager.liveness_probe():
            self.send_response(200)
            self.end_headers()
        else :
            self.send_response(500)
            self.end_headers()

    def ready_probe(self):

        if ConnectionManager.liveness_probe():
            self.send_response(200)
            self.end_headers()
        else :
            self.send_response(500)
            self.end_headers()
