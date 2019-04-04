#!/usr/bin/env python
#
# Copyright 2016 the original author or authors.
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

"""
Device level CLI commands
"""
from optparse import make_option
from cmd2 import Cmd, options
from simplejson import dumps

from table import print_pb_as_table, print_pb_list_as_table
from utils import print_flows, pb2dict, enum2name

from voltha_protos import voltha_pb2, common_pb2
import sys
import json
from google.protobuf.json_format import MessageToDict

# Since proto3 won't send fields that are set to 0/false/"" any object that
# might have those values set in them needs to be replicated here such that the
# fields can be adequately


class DeviceCli(Cmd):

    def __init__(self, device_id, get_stub):
        Cmd.__init__(self)
        self.get_stub = get_stub
        self.device_id = device_id
        self.prompt = '(' + self.colorize(
            self.colorize('device {}'.format(device_id), 'red'), 'bold') + ') '
        self.pm_config_last = None
        self.pm_config_dirty = False

    def cmdloop(self):
        self._cmdloop()

    def get_device(self, depth=0):
        stub = self.get_stub()
        res = stub.GetDevice(voltha_pb2.ID(id=self.device_id),
                             metadata=(('get-depth', str(depth)), ))
        return res

    do_exit = Cmd.do_quit

    def do_quit(self, line):
        if self.pm_config_dirty:
            self.poutput("Uncommited changes for " + \
                         self.colorize(
                             self.colorize("perf_config,", "blue"),
                             "bold") + " please either " + self.colorize(
                             self.colorize("commit", "blue"), "bold") + \
                         " or " + self.colorize(
                             self.colorize("reset", "blue"), "bold") + \
                         " your changes using " + \
                         self.colorize(
                             self.colorize("perf_config", "blue"), "bold"))
            return False
        else:
            return self._STOP_AND_EXIT

    def do_show(self, line):
        """Show detailed device information"""
        print_pb_as_table('Device {}'.format(self.device_id),
                          self.get_device(depth=-1))

    def do_ports(self, line):
        """Show ports of device"""
        device = self.get_device(depth=-1)
        omit_fields = {
        }
        print_pb_list_as_table('Device ports:', device.ports,
                               omit_fields, self.poutput)

    def complete_perf_config(self, text, line, begidx, endidx):
        sub_cmds = {"show", "set", "commit", "reset"}
        sub_opts = {"-f", "-e", "-d", "-o"}
        # Help the interpreter complete the paramters.
        completions = []
        if not self.pm_config_last:
            device = self.get_device(depth=-1)
            self.pm_config_last = device.pm_configs
        m_names = [d.name for d in self.pm_config_last.metrics]
        cur_cmd = line.strip().split(" ")
        try:
            if not text and len(cur_cmd) == 1:
                completions = ("show", "set", "commit", "reset")
            elif len(cur_cmd) == 2:
                if "set" == cur_cmd[1]:
                    completions = [d for d in sub_opts]
                else:
                    completions = [d for d in sub_cmds if d.startswith(text)]
            elif len(cur_cmd) > 2 and cur_cmd[1] == "set":
                if cur_cmd[len(cur_cmd)-1] == "-":
                    completions = [list(d)[1] for d in sub_opts]
                elif cur_cmd[len(cur_cmd)-1] == "-f":
                    completions = ("\255","Please enter a sampling frequency in 10ths of a second")
                elif cur_cmd[len(cur_cmd)-2] == "-f":
                    completions = [d for d in sub_opts]
                elif cur_cmd[len(cur_cmd)-1] in {"-e","-d","-o"}:
                    if self.pm_config_last.grouped:
                        pass
                    else:
                        completions = [d.name for d in self.pm_config_last.metrics]
                elif cur_cmd[len(cur_cmd)-2] in {"-e","-d"}:
                    if text and text not in m_names:
                        completions = [d for d in m_names if d.startswith(text)]
                    else:
                        completions = [d for d in sub_opts]
                elif cur_cmd[len(cur_cmd)-2] == "-o":
                    if cur_cmd[len(cur_cmd)-1] in [d.name for d in self.pm_config_last.metrics]:
                        completions = ("\255","Please enter a sampling frequency in 10ths of a second")
                    else:
                        completions = [d for d in m_names if d.startswith(text)]
                elif cur_cmd[len(cur_cmd)-3] == "-o":
                    completions = [d for d in sub_opts]
        except:
            e = sys.exc_info()
            print(e)
        return completions


    def help_perf_config(self):
        self.poutput(
'''
perf_config [show | set | commit | reset] [-f <default frequency>] [{-e <metric/group
            name>}] [{-d <metric/group name>}] [{-o <metric/group name> <override
            frequency>}]

show: displays the performance configuration of the device
set: changes the parameters specified with -e, -d, and -o
reset: reverts any changes made since the last commit
commit: commits any changes made which applies them to the device.

-e: enable collection of the specified metric, more than one -e may be
            specified.
-d: disable collection of the specified metric, more than on -d may be
            specified.
-o: override the collection frequency of the specified metric, more than one -o
            may be specified. Note that -o isn't valid unless
            frequency_override is set to True for the device.

Changes made by set are held locally until a commit or reset command is issued.
A commit command will write the configuration to the device and it takes effect
immediately. The reset command will undo any changes since the start of the
device session.

If grouped is true then the -d, -e and -o commands refer to groups and not
individual metrics.
'''
        )

    @options([
        make_option('-f', '--default_freq', action="store", dest='default_freq',
                    type='long', default=None),
        make_option('-e', '--enable', action='append', dest='enable',
                    default=None),
        make_option('-d', '--disable', action='append', dest='disable',
                    default=None),
        make_option('-o', '--override', action='append', dest='override',
                    nargs=2, default=None, type='string'),
    ])
    def do_perf_config(self, line, opts):
        """Show and set the performance monitoring configuration of the device"""

        device = self.get_device(depth=-1)
        if not self.pm_config_last:
            self.pm_config_last = device.pm_configs

        # Ensure that a valid sub-command was provided
        if line.strip() not in {"set", "show", "commit", "reset", ""}:
                self.poutput(self.colorize('Error: ', 'red') +
                             self.colorize(self.colorize(line.strip(), 'blue'),
                                           'bold') + ' is not recognized')
                return

        # Ensure no options are provided when requesting to view the config
        if line.strip() == "show" or line.strip() == "":
            if opts.default_freq or opts.enable or opts.disable:
                self.poutput(opts.disable)
                self.poutput(self.colorize('Error: ', 'red') + 'use ' +
                             self.colorize(self.colorize('"set"', 'blue'),
                                           'bold') + ' to change settings')
                return

        if line.strip() == "set":  # Set the supplied values
            metric_list = set()
            if opts.enable is not None:
                metric_list |= {metric for metric in opts.enable}
            if opts.disable is not None:
                metric_list |= {metric for metric in opts.disable}
            if opts.override is not None:
                metric_list |= {metric for metric, _ in opts.override}

            # The default frequency
            if opts.default_freq:
                self.pm_config_last.default_freq = opts.default_freq
                self.pm_config_dirty = True

            # Field or group visibility
            if self.pm_config_last.grouped:
                for g in self.pm_config_last.groups:
                    if opts.enable:
                        if g.group_name in opts.enable:
                            g.enabled = True
                            self.pm_config_dirty = True
                            metric_list.discard(g.group_name)
                for g in self.pm_config_last.groups:
                    if opts.disable:
                        if g.group_name in opts.disable:
                            g.enabled = False
                            self.pm_config_dirty = True
                            metric_list.discard(g.group_name)
            else:
                for m in self.pm_config_last.metrics:
                    if opts.enable:
                        if m.name in opts.enable:
                            m.enabled = True
                            self.pm_config_dirty = True
                            metric_list.discard(m.name)
                for m in self.pm_config_last.metrics:
                    if opts.disable:
                        if m.name in opts.disable:
                            m.enabled = False
                            self.pm_config_dirty = True
                            metric_list.discard(m.name)

            # Frequency overrides.
            if opts.override:
                if self.pm_config_last.freq_override:
                    oo = dict()
                    for o in opts.override:
                        oo[o[0]] = o[1]
                    if self.pm_config_last.grouped:
                        for g in self.pm_config_last.groups:
                            if g.group_name in oo:
                                try:
                                    g.group_freq = int(oo[g.group_name])
                                except ValueError:
                                    self.poutput(self.colorize('Warning: ',
                                                               'yellow') +
                                                 self.colorize(oo[g.group_name],
                                                               'blue') +
                                                 " is not an integer... ignored")
                                del oo[g.group_name]
                                self.pm_config_dirty = True
                                metric_list.discard(g.group_name)
                    else:
                        for m in self.pm_config_last.metrics:
                            if m.name in oo:
                                try:
                                    m.sample_freq = int(oo[m.name])
                                except ValueError:
                                    self.poutput(self.colorize('Warning: ',
                                                               'yellow') +
                                                 self.colorize(oo[m.name],
                                                               'blue') +
                                                 " is not an integer... ignored")
                                del oo[m.name]
                                self.pm_config_dirty = True
                                metric_list.discard(m.name)

                    # If there's anything left the input was typoed
                    if self.pm_config_last.grouped:
                        field = 'group'
                    else:
                        field = 'metric'
                    for o in oo:
                        self.poutput(self.colorize('Warning: ', 'yellow') +
                                     'the parameter' + ' ' +
                                     self.colorize(o, 'blue') + ' is not ' +
                                     'a ' + field + ' name... ignored')
                    if oo:
                        return

                else:  # Frequency overrides not enabled
                    self.poutput(self.colorize('Error: ', 'red') +
                                 'Individual overrides are only ' +
                                 'supported if ' +
                                 self.colorize('freq_override', 'blue') +
                                 ' is set to ' + self.colorize('True', 'blue'))
                    return

            if len(metric_list):
                metric_name_list = ", ".join(str(metric) for metric in metric_list)
                self.poutput(self.colorize('Error: ', 'red') +
                             'Metric/Metric Group{} '.format('s' if len(metric_list) > 1 else '') +
                             self.colorize(metric_name_list, 'blue') +
                             ' {} not found'.format('were' if len(metric_list) > 1 else 'was'))
                return

            self.poutput("Success")
            return

        elif line.strip() == "commit" and self.pm_config_dirty:
            stub = self.get_stub()
            stub.UpdateDevicePmConfigs(self.pm_config_last)
            self.pm_config_last = self.get_device(depth=-1).pm_configs
            self.pm_config_dirty = False

        elif line.strip() == "reset" and self.pm_config_dirty:
            self.pm_config_last = self.get_device(depth=-1).pm_configs
            self.pm_config_dirty = False

        omit_fields = {'groups', 'metrics', 'id'}
        print_pb_as_table('PM Config:', self.pm_config_last, omit_fields,
                          self.poutput,show_nulls=True)
        if self.pm_config_last.grouped:
            #self.poutput("Supported metric groups:")
            for g in self.pm_config_last.groups:
                if self.pm_config_last.freq_override:
                    omit_fields = {'metrics'}
                else:
                    omit_fields = {'group_freq','metrics'}
                print_pb_as_table('', g, omit_fields, self.poutput,
                                  show_nulls=True)
                if g.enabled:
                    state = 'enabled'
                else:
                    state = 'disabled'
                print_pb_list_as_table(
                    'Metric group {} is {}'.format(g.group_name,state),
                    g.metrics, {'enabled', 'sample_freq'}, self.poutput,
                    dividers=100, show_nulls=True)
        else:
            if self.pm_config_last.freq_override:
                omit_fields = {}
            else:
                omit_fields = {'sample_freq'}
            print_pb_list_as_table('Supported metrics:', self.pm_config_last.metrics,
                                   omit_fields, self.poutput, dividers=100,
                                   show_nulls=True)

    def do_flows(self, line):
        """Show flow table for device"""
        device = pb2dict(self.get_device(-1))
        print_flows(
            'Device',
            self.device_id,
            type=device['type'],
            flows=device['flows']['items'],
            groups=device['flow_groups']['items']
        )

    def do_images(self, line):
        """Show software images on the device"""
        device = self.get_device(depth=-1)
        omit_fields = {}
        print_pb_list_as_table('Software Images:', device.images.image,
                               omit_fields, self.poutput, show_nulls=True)

    @options([
        make_option('-u', '--url', action='store', dest='url',
                    help="URL to get sw image"),
        make_option('-n', '--name', action='store', dest='name',
                    help="Image name"),
        make_option('-c', '--crc', action='store', dest='crc',
                    help="CRC code to verify with", default=0),
        make_option('-v', '--version', action='store', dest='version',
                    help="Image version", default=0),
    ])
    def do_img_dnld_request(self, line, opts):
        """
        Request image download to a device
        """
        device = self.get_device(depth=-1)
        self.poutput('device_id {}'.format(device.id))
        self.poutput('name {}'.format(opts.name))
        self.poutput('url {}'.format(opts.url))
        self.poutput('crc {}'.format(opts.crc))
        self.poutput('version {}'.format(opts.version))
        try:
            device_id = device.id
            if device_id and opts.name and opts.url:
                kw = dict(id=device_id)
                kw['name'] = opts.name
                kw['url'] = opts.url
            else:
                self.poutput('Device ID and URL are needed')
                raise Exception('Device ID and URL are needed')
        except Exception as e:
            self.poutput('Error request img dnld {}.  Error:{}'.format(device_id, e))
            return
        kw['crc'] = long(opts.crc)
        kw['image_version'] = opts.version
        response = None
        try:
            request = voltha_pb2.ImageDownload(**kw)
            stub = self.get_stub()
            response = stub.DownloadImage(request)
        except Exception as e:
            self.poutput('Error download image {}. Error:{}'.format(kw['id'], e))
            return
        name = enum2name(common_pb2.OperationResp,
                        'OperationReturnCode', response.code)
        self.poutput('response: {}'.format(name))
        self.poutput('{}'.format(response))

    @options([
        make_option('-n', '--name', action='store', dest='name',
                    help="Image name"),
    ])
    def do_img_dnld_status(self, line, opts):
        """
        Get a image download status
        """
        device = self.get_device(depth=-1)
        self.poutput('device_id {}'.format(device.id))
        self.poutput('name {}'.format(opts.name))
        try:
            device_id = device.id
            if device_id and opts.name:
                kw = dict(id=device_id)
                kw['name'] = opts.name
            else:
                self.poutput('Device ID, Image Name are needed')
                raise Exception('Device ID, Image Name are needed')
        except Exception as e:
            self.poutput('Error get img dnld status {}.  Error:{}'.format(device_id, e))
            return
        status = None
        try:
            img_dnld = voltha_pb2.ImageDownload(**kw)
            stub = self.get_stub()
            status = stub.GetImageDownloadStatus(img_dnld)
        except Exception as e:
            self.poutput('Error get img dnld status {}. Error:{}'.format(device_id, e))
            return
        fields_to_omit = {
              'crc',
              'local_dir',
        }
        try:
            print_pb_as_table('ImageDownload Status:', status, fields_to_omit, self.poutput)
        except Exception, e:
            self.poutput('Error {}.  Error:{}'.format(device_id, e))

    def do_img_dnld_list(self, line):
        """
        List all image download records for a given device
        """
        device = self.get_device(depth=-1)
        device_id = device.id
        self.poutput('Get all img dnld records {}'.format(device_id))
        try:
            stub = self.get_stub()
            img_dnlds = stub.ListImageDownloads(voltha_pb2.ID(id=device_id))
        except Exception, e:
            self.poutput('Error list img dnlds {}.  Error:{}'.format(device_id, e))
            return
        fields_to_omit = {
              'crc',
              'local_dir',
        }
        try:
            print_pb_list_as_table('ImageDownloads:', img_dnlds.items, fields_to_omit, self.poutput)
        except Exception, e:
            self.poutput('Error {}.  Error:{}'.format(device_id, e))


    @options([
        make_option('-n', '--name', action='store', dest='name',
                    help="Image name"),
    ])
    def do_img_dnld_cancel(self, line, opts):
        """
        Cancel a requested image download
        """
        device = self.get_device(depth=-1)
        self.poutput('device_id {}'.format(device.id))
        self.poutput('name {}'.format(opts.name))
        device_id = device.id
        try:
            if device_id and opts.name:
                kw = dict(id=device_id)
                kw['name'] = opts.name
            else:
                self.poutput('Device ID, Image Name are needed')
                raise Exception('Device ID, Image Name are needed')
        except Exception as e:
            self.poutput('Error cancel sw dnld {}. Error:{}'.format(device_id, e))
            return
        response = None
        try:
            img_dnld = voltha_pb2.ImageDownload(**kw)
            stub = self.get_stub()
            img_dnld = stub.GetImageDownload(img_dnld)
            response = stub.CancelImageDownload(img_dnld)
        except Exception as e:
            self.poutput('Error cancel sw dnld {}. Error:{}'.format(device_id, e))
            return
        name = enum2name(common_pb2.OperationResp,
                        'OperationReturnCode', response.code)
        self.poutput('response: {}'.format(name))
        self.poutput('{}'.format(response))

    @options([
        make_option('-n', '--name', action='store', dest='name',
                    help="Image name"),
        make_option('-s', '--save', action='store', dest='save_config',
                    help="Save Config", default="True"),
        make_option('-d', '--dir', action='store', dest='local_dir',
                    help="Image on device location"),
    ])
    def do_img_activate(self, line, opts):
        """
        Activate an image update on device
        """
        device = self.get_device(depth=-1)
        device_id = device.id
        try:
            if device_id and opts.name and opts.local_dir:
                kw = dict(id=device_id)
                kw['name'] = opts.name
                kw['local_dir'] = opts.local_dir
            else:
                self.poutput('Device ID, Image Name, and Location are needed')
                raise Exception('Device ID, Image Name, and Location are needed')
        except Exception as e:
            self.poutput('Error activate image {}. Error:{}'.format(device_id, e))
            return
        kw['save_config'] = json.loads(opts.save_config.lower())
        self.poutput('activate image update {} {} {} {}'.format( \
                    kw['id'], kw['name'],
                    kw['local_dir'], kw['save_config']))
        response = None
        try:
            img_dnld = voltha_pb2.ImageDownload(**kw)
            stub = self.get_stub()
            img_dnld = stub.GetImageDownload(img_dnld)
            response = stub.ActivateImageUpdate(img_dnld)
        except Exception as e:
            self.poutput('Error activate image {}. Error:{}'.format(kw['id'], e))
            return
        name = enum2name(common_pb2.OperationResp,
                        'OperationReturnCode', response.code)
        self.poutput('response: {}'.format(name))
        self.poutput('{}'.format(response))

    @options([
        make_option('-n', '--name', action='store', dest='name',
                    help="Image name"),
        make_option('-s', '--save', action='store', dest='save_config',
                    help="Save Config", default="True"),
        make_option('-d', '--dir', action='store', dest='local_dir',
                    help="Image on device location"),
    ])
    def do_img_revert(self, line, opts):
        """
        Revert an image update on device
        """
        device = self.get_device(depth=-1)
        device_id = device.id
        try:
            if device_id and opts.name and opts.local_dir:
                kw = dict(id=device_id)
                kw['name'] = opts.name
                kw['local_dir'] = opts.local_dir
            else:
                self.poutput('Device ID, Image Name, and Location are needed')
                raise Exception('Device ID, Image Name, and Location are needed')
        except Exception as e:
            self.poutput('Error revert image {}. Error:{}'.format(device_id, e))
            return
        kw['save_config'] = json.loads(opts.save_config.lower())
        self.poutput('revert image update {} {} {} {}'.format( \
                    kw['id'], kw['name'],
                    kw['local_dir'], kw['save_config']))
        response = None
        try:
            img_dnld = voltha_pb2.ImageDownload(**kw)
            stub = self.get_stub()
            img_dnld = stub.GetImageDownload(img_dnld)
            response = stub.RevertImageUpdate(img_dnld)
        except Exception as e:
            self.poutput('Error revert image {}. Error:{}'.format(kw['id'], e))
            return
        name = enum2name(common_pb2.OperationResp,
                        'OperationReturnCode', response.code)
        self.poutput('response: {}'.format(name))
        self.poutput('{}'.format(response))
