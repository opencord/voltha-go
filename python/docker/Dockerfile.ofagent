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
FROM voltha/voltha-python-base:1.0.0

# Install adapter requirements.
COPY requirements.txt /tmp/requirements.txt
RUN pip install -r /tmp/requirements.txt

ARG LOCAL_PYVOLTHA
ARG LOCAL_PROTOS
COPY local_imports/ /local_imports/
RUN if [ -n "$LOCAL_PYVOLTHA" ] ; then \
    PYVOLTHA_PATH=$(ls /local_imports/pyvoltha/dist/) ; \
    printf "/local_imports/pyvoltha/dist/%s\npyvoltha" "$PYVOLTHA_PATH" > pyvoltha-install.txt ; \
    pip install -r pyvoltha-install.txt ; \
fi

RUN if [ -n "$LOCAL_PROTOS" ] ; then \
    PROTOS_PATH=$(ls /local_imports/voltha-protos/dist/) ; \
    printf "/local_imports/voltha-protos/dist/%s\nvoltha-protos" "$PROTOS_PATH" > protos-install.txt ; \
    pip install -r protos-install.txt ; \
 fi

# Bundle app source
RUN mkdir /ofagent  && \
        touch   /ofagent/__init__.py

ENV PYTHONPATH=/ofagent
COPY ofagent /ofagent/ofagent
COPY pki /ofagent/pki

# Label image
ARG org_label_schema_version=unknown
ARG org_label_schema_vcs_url=unknown
ARG org_label_schema_vcs_ref=unknown
ARG org_label_schema_build_date=unknown
ARG org_opencord_vcs_commit_date=unknown

LABEL org.label-schema.schema-version=1.0 \
      org.label-schema.name=voltha-ofagent \
      org.label-schema.version=$org_label_schema_version \
      org.label-schema.vcs-url=$org_label_schema_vcs_url \
      org.label-schema.vcs-ref=$org_label_schema_vcs_ref \
      org.label-schema.build-date=$org_label_schema_build_date \
      org.opencord.vcs-commit-date=$org_opencord_vcs_commit_date
