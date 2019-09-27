#
# Copyright 2017 the original author or authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
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
