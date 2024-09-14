#!/bin/sh

#while true ; do
#  clear
#  kubectl get -o jsonpath="{.status}" temporalworker "$1" | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | yq
#  sleep 2
#done

#refresh() {
#  tput clear || exit 2; # Clear screen. Almost same as echo -en '\033[2J';
#  bash -ic "$@";
#}
#
#while true; do
##   CMD="kubectl get -o jsonpath=\"{.status}\" temporalworker $1 | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken'";
#   # Cache output to prevent flicker. Assigning to variable
#   # also removes trailing newline.
#   output=$(refresh "kubectl get -o jsonpath=\"{.status}\" temporalworker \"$1\" | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken'");
#   # Exit if ^C was pressed while command was executing or there was an error.
#   exitcode=$?; [ $exitcode -ne 0 ] && exit $exitcode
#   printf '%s' "$output" | yq;  # Almost the same as echo $output
#   sleep 1;
#done;

refresh() {
  tput clear || exit 2; # Clear screen. Almost same as echo -en '\033[2J';
  bash -ic "$@";
}

# Like watch, but with color
cwatch() {
   while true; do
     CMD="$@";
     # Cache output to prevent flicker. Assigning to variable
     # also removes trailing newline.
     output=`refresh "$CMD"`;
     # Exit if ^C was pressed while command was executing or there was an error.
     exitcode=$?; [ $exitcode -ne 0 ] && exit $exitcode
     printf '%s' "$output";  # Almost the same as echo $output
     sleep 3;
   done;
}

#watch --color "kubectl get -o jsonpath=\"{.status}\" temporalworker $1 | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | bat -l yaml --color always --plain --theme OneHalfDark"
cwatch "kubectl get -o jsonpath=\"{.status}\" temporalworker $1 | yq -p json -o yaml | grep -v -E 'apiVersion|resourceVersion|kind|uid|namespace|deployment|name|versionConflictToken' | bat -l yaml --color always --plain --wrap never --theme gruvbox-dark"

#watch --color "ls -a1 --color"