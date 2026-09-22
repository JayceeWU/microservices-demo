# Copyright 2021 Google LLC
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

# Define a default value so it's not empty if the builder fails to provide it
ARG BUILDPLATFORM=linux/amd64

# https://mcr.microsoft.com/product/dotnet/sdk
FROM --platform=$BUILDPLATFORM mcr.microsoft.com/dotnet/sdk:9.0 AS builder
ARG TARGETARCH=amd64
WORKDIR /app
COPY src/cartservice/src/cartservice.csproj src/cartservice/src/
WORKDIR /app/src/cartservice/src
RUN dotnet restore cartservice.csproj \
    -a $TARGETARCH
COPY src/cartservice/src .
RUN dotnet publish cartservice.csproj \
    -p:PublishSingleFile=true \
    -a $TARGETARCH \
    --self-contained true \
    -p:PublishTrimmed=true \
    -p:TrimMode=full \
    -c release \
    -o /cartservice

# https://mcr.microsoft.com/product/dotnet/runtime-deps
FROM mcr.microsoft.com/dotnet/runtime-deps:9.0-noble-chiseled

WORKDIR /app
COPY --from=builder /cartservice .
EXPOSE 8080 9090
ENV DOTNET_EnableDiagnostics=0 \
    ASPNETCORE_HTTP_PORTS=8080
USER 1000:1000
ENTRYPOINT ["/app/cartservice"]
