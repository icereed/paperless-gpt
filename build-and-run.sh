#!/usr/bin/env bash
set -o allexport
source .env
set +o allexport

go build
./paperless-gpt
