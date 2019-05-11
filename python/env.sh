# Copyright 2017-present Open Networking Foundation
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
# sourcing this file is needed to make local development and integration testing work

# load local python virtualenv if exists
VENVDIR="venv-volthago"
if [ -e "$VENVDIR/bin/activate" ]; then
    . $VENVDIR/bin/activate
else
   echo "Run 'make venv' to setup python development environment"
fi
