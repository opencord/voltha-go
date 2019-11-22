#!/usr/bin/env python
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
import argparse
import os

import yaml
import SocketServer

from probe import Probe
from twisted.internet import reactor
from twisted.internet.defer import inlineCallbacks

from pyvoltha.common.structlog_setup import setup_logging
from pyvoltha.common.utils.dockerhelpers import get_my_containers_name
from pyvoltha.common.utils.nethelpers import get_my_primary_local_ipv4
from connection_mgr import ConnectionManager

defs = dict(
    config=os.environ.get('CONFIG', './ofagent.yml'),
    consul=os.environ.get('CONSUL', 'localhost:8500'),
    controller=os.environ.get('CONTROLLER', 'localhost:6653'),
    probe=os.environ.get('PROBE', ':8080'),
    external_host_address=os.environ.get('EXTERNAL_HOST_ADDRESS',
                                         get_my_primary_local_ipv4()),
    grpc_endpoint=os.environ.get('GRPC_ENDPOINT', 'localhost:50055'),
    grpc_timeout=os.environ.get('GRPC_TIMEOUT', '10'),
    core_binding_key=os.environ.get('CORE_BINDING_KEY', 'voltha_backend_name'),
    core_transaction_key=os.environ.get('CORE_TRANSACTION_KEY', 'voltha_serial_number'),
    instance_id=os.environ.get('INSTANCE_ID', os.environ.get('HOSTNAME', '1')),
    internal_host_address=os.environ.get('INTERNAL_HOST_ADDRESS',
                                         get_my_primary_local_ipv4()),
    work_dir=os.environ.get('WORK_DIR', '/tmp/ofagent'),
    key_file=os.environ.get('KEY_FILE', '/ofagent/pki/voltha.key'),
    cert_file=os.environ.get('CERT_FILE', '/ofagent/pki/voltha.crt')
)


def parse_args():

    parser = argparse.ArgumentParser()

    _help = ('Path to ofagent.yml config file (default: %s). '
             'If relative, it is relative to main.py of ofagent.'
             % defs['config'])
    parser.add_argument('-c', '--config',
                        dest='config',
                        action='store',
                        default=defs['config'],
                        help=_help)

    _help = '<hostname>:<port> to consul agent (default: %s)' % defs['consul']
    parser.add_argument(
        '-C', '--consul', dest='consul', action='store',
        default=defs['consul'],
        help=_help)

    _help = '<hostname1>:<port1> <hostname2>:<port2> <hostname3>:<port3> ... <hostnamen>:<portn>   to openflow controller (default: %s)' % \
            defs['controller']
    parser.add_argument(
        '-O', '--controller',nargs = '*', dest='controller', action='store',
        default=defs['controller'],
        help=_help)

    _help = '<hostname>:<port> for liveness and readiness probes (default: %s)' % defs['probe']
    parser.add_argument(
        '-P', '--probe', dest='probe', action='store',
        default=defs['probe'],
        help=_help)

    _help = ('<hostname> or <ip> at which ofagent is reachable from outside '
             'the cluster (default: %s)' % defs['external_host_address'])
    parser.add_argument('-E', '--external-host-address',
                        dest='external_host_address',
                        action='store',
                        default=defs['external_host_address'],
                        help=_help)

    _help = ('gRPC end-point to connect to. It can either be a direct '
             'definition in the form of <hostname>:<port>, or it can be an '
             'indirect definition in the form of @<service-name> where '
             '<service-name> is the name of the grpc service as registered '
             'in consul (example: @voltha-grpc). (default: %s)'
             % defs['grpc_endpoint'])
    parser.add_argument('-G', '--grpc-endpoint',
                        dest='grpc_endpoint',
                        action='store',
                        default=defs['grpc_endpoint'],
                        help=_help)

    _help = 'gRPC timeout in seconds (default: %s)' % defs['grpc_timeout']
    parser.add_argument('-T', '--grpc-timeout',
                        dest='grpc_timeout',
                        action='store',
                        default=defs['grpc_timeout'],
                        help=_help)

    _help = ('The name of the meta-key whose value is the rw-core group '
             'to which the ofagent\'s gRPC client is bound. '
             '(default: %s)' % defs['core_binding_key'])
    parser.add_argument('-B', '--core-binding-key',
                        dest='core_binding_key',
                        action='store',
                        default=defs['core_binding_key'],
                        help=_help)

    _help = ('The name of the meta-key whose value is the transaction ID '
             'used by the OFAgent to send requests to the Voltha RW Core. '
             '(default: %s)' % defs['core_transaction_key'])
    parser.add_argument('-ctk', '--core_transaction_key',
                        dest='core_transaction_key',
                        action='store',
                        default=defs['core_transaction_key'],
                        help=_help)

    _help = ('<hostname> or <ip> at which ofagent is reachable from inside '
             'the cluster (default: %s)' % defs['internal_host_address'])
    parser.add_argument('-H', '--internal-host-address',
                        dest='internal_host_address',
                        action='store',
                        default=defs['internal_host_address'],
                        help=_help)

    _help = ('unique string id of this ofagent instance (default: %s)'
             % defs['instance_id'])
    parser.add_argument('-i', '--instance-id',
                        dest='instance_id',
                        action='store',
                        default=defs['instance_id'],
                        help=_help)

    _help = 'omit startup banner log lines'
    parser.add_argument('-n', '--no-banner',
                        dest='no_banner',
                        action='store_true',
                        default=False,
                        help=_help)

    _help = "suppress debug and info logs"
    parser.add_argument('-q', '--quiet',
                        dest='quiet',
                        action='count',
                        help=_help)

    _help = 'enable verbose logging'
    parser.add_argument('-v', '--verbose',
                        dest='verbose',
                        action='count',
                        help=_help)

    _help = ('work dir to compile and assemble generated files (default=%s)'
             % defs['work_dir'])
    parser.add_argument('-w', '--work-dir',
                        dest='work_dir',
                        action='store',
                        default=defs['work_dir'],
                        help=_help)

    _help = ('use docker container name as ofagent instance id'
             ' (overrides -i/--instance-id option)')
    parser.add_argument('--instance-id-is-container-name',
                        dest='instance_id_is_container_name',
                        action='store_true',
                        default=False,
                        help=_help)

    _help = ('Specify this option to enable TLS security between ofagent \
              and onos.')
    parser.add_argument('-t', '--enable-tls',
                        dest='enable_tls',
                        action='store_true',
                        help=_help)

    _help = ('key file to be used for tls security (default=%s)'
             % defs['key_file'])
    parser.add_argument('-k', '--key-file',
                        dest='key_file',
                        action='store',
                        default=defs['key_file'],
                        help=_help)

    _help = ('certificate file to be used for tls security (default=%s)'
             % defs['cert_file'])
    parser.add_argument('-r', '--cert-file',
                        dest='cert_file',
                        action='store',
                        default=defs['cert_file'],
                        help=_help)

    args = parser.parse_args()

    # post-processing

    if args.instance_id_is_container_name:
        args.instance_id = get_my_containers_name()

    return args


def load_config(args):
    path = args.config
    if path.startswith('.'):
        dir = os.path.dirname(os.path.abspath(__file__))
        path = os.path.join(dir, path)
    path = os.path.abspath(path)
    with open(path) as fd:
        config = yaml.load(fd)
    return config

banner = r'''
  ___  _____ _                    _
 / _ \|  ___/ \   __ _  ___ _ __ | |_
| | | | |_ / _ \ / _` |/ _ \ '_ \| __|
| |_| |  _/ ___ \ (_| |  __/ | | | |_
 \___/|_|/_/   \_\__, |\___|_| |_|\__|
                 |___/
'''

def print_banner(log):
    for line in banner.strip('\n').splitlines():
        log.info(line)
    log.info('(to stop: press Ctrl-C)')


class Main(object):

    def __init__(self):

        self.args = args = parse_args()
        self.config = load_config(args)
        self.grpc_timeout = int(self.args.grpc_timeout)

        verbosity_adjust = (args.verbose or 0) - (args.quiet or 0)
        self.log = setup_logging(self.config.get('logging', {}),
                                 args.instance_id,
                                 verbosity_adjust=verbosity_adjust)

        # components
        self.connection_manager = None

        self.exiting = False

        if not args.no_banner:
            print_banner(self.log)

        self.startup_components()

    def start(self):
        self.start_reactor()  # will not return except Keyboard interrupt

    @inlineCallbacks
    def startup_components(self):
        self.log.info('starting-internal-components')
        args = self.args
        self.connection_manager = yield ConnectionManager(
            args.consul,
            args.grpc_endpoint,
            self.grpc_timeout,
            args.core_binding_key,
            args.core_transaction_key,
            args.controller,
            args.instance_id,
            args.enable_tls,
            args.key_file,
            args.cert_file).start()
        Probe.connection_manager = self.connection_manager
        self.log.info('started-internal-services')

    @inlineCallbacks
    def shutdown_components(self):
        """Execute before the reactor is shut down"""
        self.log.info('exiting-on-keyboard-interrupt')
        self.exiting = True
        if self.connection_manager is not None:
            yield self.connection_manager.stop()

    def start_reactor(self):
        reactor.callWhenRunning(
            lambda: self.log.info('twisted-reactor-started'))

        reactor.addSystemEventTrigger('before', 'shutdown',
                                      self.shutdown_components)
        reactor.callInThread(self.start_probe)
        reactor.run()

    def start_probe(self):
        args = self.args
        host = args.probe.split(':')[0]
        port = args.probe.split(':')[1]
        server = SocketServer.TCPServer((host, int(port)), Probe)
        server.serve_forever()

if __name__ == '__main__':
    Main().start()
