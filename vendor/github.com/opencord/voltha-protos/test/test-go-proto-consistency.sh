#!/usr/bin/env bash

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

# test-go-proto-consistency.sh
#
# This test validates that go proto outputs are committed to git.  It does this
# by regenerating them and validating whether git thinks they are the same. If
# they become out of sync, there can be problems for consumers of the protos.

set -eu -o pipefail

git status > /dev/null
STAGED="$(git diff-index --quiet HEAD -- || echo 'staged')"
UNTRACKED="$(git ls-files --exclude-standard --others)"

if [ "$STAGED" == "staged" ] || [ "$UNTRACKED" != "" ]; then
    echo "Please commit or ignore local changes before executing this test"
    git status
    exit 1
fi

# delete and regenerate go protos
rm -rf go/voltha.pb go/*/*.pb.go go_temp
make go-protos

# Running git status ensures correct git diff-index picks up changes
git status > /dev/null
STAGED_POST="$(git diff-index --quiet HEAD -- || echo "staged")"
UNTRACKED_POST="$(git ls-files --exclude-standard --others)"

if [ "$STAGED_POST" == "staged" ] || [ "$UNTRACKED_POST" != "" ] ; then
    echo "You have go proto build outputs that are not committed."
    echo "Check git status and commit updated files."
    git status
    exit 1
else
    echo "Test successful. All go proto build outputs are committed"
    exit 0
fi

