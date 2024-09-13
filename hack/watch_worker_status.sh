#!/bin/sh

while true ; do
  clear
  kubectl get -o jsonpath="{.status}" temporalworker "$1" | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid' | yq
  sleep 5
done
