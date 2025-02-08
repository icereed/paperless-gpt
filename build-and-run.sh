#!/usr/bin/env bash
set -o allexport
source .env set
set +o allexport

go build
./paperless-gpt