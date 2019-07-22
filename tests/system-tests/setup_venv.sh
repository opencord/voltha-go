#!/usr/bin/env bash

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

# sets up a python virtualenv for running voltha system tests

WORKSPACE=${WORKSPACE:-.}
VENVDIR="${WORKSPACE}/venv-voltha-tests"

# create venv if it's not yet there
if [ ! -x "${VENVDIR}/bin/activate" ]; then
  echo "Setting up dev/test virtualenv in ${VENVDIR} for VOLTHA-System-Tests"
  virtualenv -q "${VENVDIR}" --no-site-packages
  echo "Virtualenv created."
fi

echo "Installing python requirements in virtualenv with pip"
source "${VENVDIR}/bin/activate"
pip3 install --upgrade pip
pip3 install cryptography==2.4.2 robotframework robotframework-requests robotframework-sshlibrary  \
    pexpect robotframework-httplibrary robotframework-kafkalibrary pygments pyyaml \
    robotframework-databaselibrary psycopg2==2.7.7 paramiko==2.4.2
pip3 install requests tinydb

echo "VOLTHA-System-Tests virtualenv created. Run 'source ${VENVDIR}/bin/activate'."