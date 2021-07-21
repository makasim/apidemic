#!/usr/bin/env sh

set -e
set -x

docker build --tag "makasim/apidemic:$1" .

docker push "makasim/apidemic:$1"
