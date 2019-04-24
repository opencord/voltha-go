#!/bin/sh

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

set -e

RETURN=0

GO_FMT_STATUS="PASSED"
GO_FMT_INCORRECT_FILES="$(gofmt -l $(find . -name "*.go" -not -path "./vendor/*"))"
if [[ "$GO_FMT_INCORRECT_FILES" ]]; then
  echo "#############################################"
  echo "#            go fmt check failed            #"
  echo "# Please run go fmt on the following files: #"
  echo "#############################################"
  echo "$GO_FMT_INCORRECT_FILES"

  GO_FMT_STATUS="FAILED"
  RETURN=1
else
  echo "#############"
  echo "# go fmt OK #"
  echo "#############"
fi
echo

GO_VET_STATUS="PASSED"
GO_VET_INCORRECT_FILES="$(go vet ./... 2>&1 || true)"
if [[ "$GO_VET_INCORRECT_FILES" ]]; then
  echo "########################################"
  echo "#         go vet check failed          #"
  echo "# Please correct the following issues: #"
  echo "########################################"
  echo "$GO_VET_INCORRECT_FILES"

  GO_VET_STATUS="FAILED"
  RETURN=1
else
  echo "#############"
  echo "# go vet OK #"
  echo "#############"
fi
echo

echo "############"
echo "# go tests #"
echo "############"
GO_TEST_STATUS="PASSED"
if ! go test ./...; then
  GO_TEST_STATUS="FAILED"
  RETURN=1
fi
echo

echo "######## Results ########"
echo "go fmt check ..... $GO_VET_STATUS"
echo "go vet check ..... $GO_VET_STATUS"
echo "go tests ......... $GO_TEST_STATUS"

exit "$RETURN"
