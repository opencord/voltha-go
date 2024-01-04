# -*- makefile -*-
# -----------------------------------------------------------------------
# Copyright 2022-2024 Open Networking Foundation (ONF) and the ONF Contributors
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
#
# SPDX-FileCopyrightText: 2022-2023 Open Networking Foundation (ONF) and the ONF Contributors
# SPDX-License-Identifier: Apache-2.0
# -----------------------------------------------------------------------

todo ::
	@echo "  o vendor/ is maintained by conflicting logic:"
	@echo "    - contents is checked into revision control (~duplication)."
	@echo "    - make targets rm -fr vendor/."
	@echo "    - Delete subdir and checkout from a central source as dependency."
	@echo "  o make test target not creating coverage log:"
	@echo "    - go test -coverprofile >> ./tests/results/go-test-coverage.out"
	@echo "    - open /app/tests/results/go-test-coverage.out: permission denied"
	@echo "  o make test failure"
	@echo "    - level:fatal, msg:could not create CPU profile: open grpc_profile.cpu: permission denied"

# [EOF]
