#
# Copyright 2018 the original author or authors.
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
from task import Task
from binascii import hexlify
from twisted.internet.defer import inlineCallbacks, failure, returnValue
from twisted.internet import reactor
from voltha.extensions.omci.omci_defs import ReasonCodes
from voltha.extensions.omci.omci_me import OmciFrame
from voltha.extensions.omci.omci import EntityOperations


class GetNextException(Exception):
    pass


class GetCapabilitiesFailure(Exception):
    pass


class OnuCapabilitiesTask(Task):
    """
    OpenOMCI MIB Capabilities Task

    This task requests information on supported MEs via the OMCI (ME#287)
    Managed entity.

    This task should be ran after MIB Synchronization and before any MIB
    Downloads to the ONU.

    Upon completion, the Task deferred callback is invoked with dictionary
    containing the supported managed entities and message types.

    results = {
                'supported-managed-entities': {set of supported managed entities},
                'supported-message-types': {set of supported message types}
              }
    """
    task_priority = 240
    name = "ONU Capabilities Task"

    max_mib_get_next_retries = 3
    mib_get_next_delay = 5
    DEFAULT_OCTETS_PER_MESSAGE = 29

    def __init__(self, omci_agent, device_id, omci_pdu_size=DEFAULT_OCTETS_PER_MESSAGE):
        """
        Class initialization

        :param omci_agent: (OmciAdapterAgent) OMCI Adapter agent
        :param device_id: (str) ONU Device ID
        :param omci_pdu_size: (int) OMCI Data payload size (not counting any trailers)
        """
        super(OnuCapabilitiesTask, self).__init__(OnuCapabilitiesTask.name,
                                                  omci_agent,
                                                  device_id,
                                                  priority=OnuCapabilitiesTask.task_priority)
        self._local_deferred = None
        self._device = omci_agent.get_device(device_id)
        self._pdu_size = omci_pdu_size
        self._supported_entities = set()
        self._supported_msg_types = set()

    def cancel_deferred(self):
        super(OnuCapabilitiesTask, self).cancel_deferred()

        d, self._local_deferred = self._local_deferred, None
        try:
            if d is not None and not d.called:
                d.cancel()
        except:
            pass

    @property
    def supported_managed_entities(self):
        """
        Return a set of the Managed Entity class IDs supported on this ONU

        None is returned if no MEs have been discovered

        :return: (set of ints)
        """
        return frozenset(self._supported_entities) if len(self._supported_entities) else None

    @property
    def supported_message_types(self):
        """
        Return a set of the Message Types supported on this ONU

        None is returned if no message types have been discovered

        :return: (set of EntityOperations)
        """
        return frozenset(self._supported_msg_types) if len(self._supported_msg_types) else None

    def start(self):
        """
        Start MIB Capabilities task
        """
        super(OnuCapabilitiesTask, self).start()
        self._local_deferred = reactor.callLater(0, self.perform_get_capabilities)

    def stop(self):
        """
        Shutdown MIB Capabilities task
        """
        self.log.debug('stopping')

        self.cancel_deferred()
        self._device = None
        super(OnuCapabilitiesTask, self).stop()

    @inlineCallbacks
    def perform_get_capabilities(self):
        """
        Perform the MIB Capabilities sequence.

        The sequence is to perform a Get request with the attribute mask equal
        to 'me_type_table'.  The response to this request will carry the size
        of (number of get-next sequences).

        Then a loop is entered and get-next commands are sent for each sequence
        requested.
        """
        self.log.debug('perform-get')

        try:
            self.strobe_watchdog()
            self._supported_entities = yield self.get_supported_entities()

            self.strobe_watchdog()
            self._supported_msg_types = yield self.get_supported_message_types()

            self.log.debug('get-success',
                           supported_entities=self.supported_managed_entities,
                           supported_msg_types=self.supported_message_types)
            results = {
                'supported-managed-entities': self.supported_managed_entities,
                'supported-message-types': self.supported_message_types
            }
            self.deferred.callback(results)

        except Exception as e:
            self.log.exception('perform-get', e=e)
            self.deferred.errback(failure.Failure(e))

    def get_count_from_data_buffer(self, data):
        """
        Extract the 4 octet buffer length from the OMCI PDU contents
        """
        self.log.debug('get-count-buffer', data=hexlify(data))
        return int(hexlify(data[:4]), 16)

    @inlineCallbacks
    def get_supported_entities(self):
        """
        Get the supported ME Types for this ONU.
        """
        try:
            # Get the number of requests needed
            frame = OmciFrame(me_type_table=True).get()
            self.strobe_watchdog()
            results = yield self._device.omci_cc.send(frame)

            omci_msg = results.fields['omci_message']
            status = omci_msg.fields['success_code']

            if status != ReasonCodes.Success.value:
                raise GetCapabilitiesFailure('Get count of supported entities failed with status code: {}'.
                                             format(status))
            data = omci_msg.fields['data']['me_type_table']
            count = self.get_count_from_data_buffer(bytearray(data))

            seq_no = 0
            data_buffer = bytearray(0)
            self.log.debug('me-type-count', octets=count, data=hexlify(data))

            # Start the loop
            for offset in xrange(0, count, self._pdu_size):
                frame = OmciFrame(me_type_table=seq_no).get_next()
                seq_no += 1
                self.strobe_watchdog()
                results = yield self._device.omci_cc.send(frame)

                omci_msg = results.fields['omci_message']
                status = omci_msg.fields['success_code']

                if status != ReasonCodes.Success.value:
                    raise GetCapabilitiesFailure(
                        'Get supported entities request at offset {} of {} failed with status code: {}'.
                        format(offset + 1, count, status))

                # Extract the data
                num_octets = count - offset
                if num_octets > self._pdu_size:
                    num_octets = self._pdu_size

                data = omci_msg.fields['data']['me_type_table']
                data_buffer += bytearray(data[:num_octets])

            me_types = {(data_buffer[x] << 8) + data_buffer[x + 1]
                        for x in xrange(0, len(data_buffer), 2)}
            returnValue(me_types)

        except Exception as e:
            self.log.exception('get-entities', e=e)
            self.deferred.errback(failure.Failure(e))

    @inlineCallbacks
    def get_supported_message_types(self):
        """
        Get the supported Message Types (actions) for this ONU.
        """
        try:
            # Get the number of requests needed
            frame = OmciFrame(message_type_table=True).get()
            self.strobe_watchdog()
            results = yield self._device.omci_cc.send(frame)

            omci_msg = results.fields['omci_message']
            status = omci_msg.fields['success_code']

            if status != ReasonCodes.Success.value:
                raise GetCapabilitiesFailure('Get count of supported msg types failed with status code: {}'.
                                             format(status))

            data = omci_msg.fields['data']['message_type_table']
            count = self.get_count_from_data_buffer(bytearray(data))

            seq_no = 0
            data_buffer = list()
            self.log.debug('me-type-count', octets=count, data=hexlify(data))

            # Start the loop
            for offset in xrange(0, count, self._pdu_size):
                frame = OmciFrame(message_type_table=seq_no).get_next()
                seq_no += 1
                self.strobe_watchdog()
                results = yield self._device.omci_cc.send(frame)

                omci_msg = results.fields['omci_message']
                status = omci_msg.fields['success_code']

                if status != ReasonCodes.Success.value:
                    raise GetCapabilitiesFailure(
                        'Get supported msg types request at offset {} of {} failed with status code: {}'.
                        format(offset + 1, count, status))

                # Extract the data
                num_octets = count - offset
                if num_octets > self._pdu_size:
                    num_octets = self._pdu_size

                data = omci_msg.fields['data']['message_type_table']
                data_buffer += data[:num_octets]

            def buffer_to_message_type(value):
                """
                Convert an integer value to the appropriate EntityOperations enumeration
                :param value: (int) Message type value (4..29)
                :return: (EntityOperations) Enumeration, None on failure
                """
                next((v for k, v in EntityOperations.__members__.items() if v.value == value), None)

            msg_types = {buffer_to_message_type(v) for v in data_buffer if v is not None}
            returnValue({msg_type for msg_type in msg_types if msg_type is not None})

        except Exception as e:
            self.log.exception('get-msg-types', e=e)
            self.deferred.errback(failure.Failure(e))
