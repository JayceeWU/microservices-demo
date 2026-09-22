# Copyright 2020 Google LLC
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

FROM node:22-alpine

WORKDIR /usr/src/app

COPY src/paymentservice/package*.json ./

RUN npm ci --omit=dev && npm cache clean --force

COPY src/paymentservice .
COPY protos /usr/protos

EXPOSE 8080 9090

USER 65532:65532
ENTRYPOINT [ "node", "http-server.js" ]
