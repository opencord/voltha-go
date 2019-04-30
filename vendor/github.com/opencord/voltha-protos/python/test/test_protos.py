# Copyright 2019-present Open Networking Foundation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import absolute_import, print_function
import unittest

from google.api import annotations_pb2
from voltha_protos import voltha_pb2

print("imported api annotations properly")


class TestProtos(unittest.TestCase):

    @classmethod
    def setUpClass(self):
        print("in setup")

    def test_proto_call(self):
        """ initialization """

        image_downloader = voltha_pb2.ImageDownload()

        self.assertIsNotNone(image_downloader)

