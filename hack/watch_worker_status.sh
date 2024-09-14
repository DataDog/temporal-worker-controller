#!/bin/sh

while true ; do
  clear
  kubectl get -o jsonpath="{.status}" temporalworker "$1" | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | yq
  sleep 2
done
