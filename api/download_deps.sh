#!/bin/sh

echo "downloading proto file dependencies..."

mkdir -p google/protobuf
curl -sL https://raw.githubusercontent.com/protocolbuffers/protobuf/master/src/google/protobuf/duration.proto --output google/protobuf/duration.proto
curl -sL https://raw.githubusercontent.com/protocolbuffers/protobuf/master/src/google/protobuf/timestamp.proto --output google/protobuf/timestamp.proto

mkdir -p google/api
curl -sL https://raw.githubusercontent.com/googleapis/googleapis/master/google/api/http.proto --output google/api/http.proto
curl -sL https://raw.githubusercontent.com/googleapis/googleapis/master/google/api/annotations.proto --output google/api/annotations.proto
