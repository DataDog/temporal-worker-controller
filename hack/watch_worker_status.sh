#!/bin/sh

while true ; do
  tmp_file="$(mktemp)"
  kubectl get -o jsonpath="{.status}" temporalworker "$1" | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' > "$tmp_file"
  clear
  cat "$tmp_file" | yq
  sleep 2
done
