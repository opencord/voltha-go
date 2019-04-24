#!/bin/sh

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
