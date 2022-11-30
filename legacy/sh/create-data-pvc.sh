#!/bin/sh -a

exec 3>&1 1>&2

data_pvc="$1"
data_sc="$2"
size="$3"

envsubst < tmpl/data-pvc.yaml | kubectl apply -f -

SECONDS=0
uid=
while [ -z "$uid" ] ; do
    uid="$( kubectl get -n volume-nfs pvc "$data_pvc" -o jsonpath='{.metadata.uid}' )"
    sleep 2
    [ "$SECONDS" -ge 30 ] && echo 'Cannot get pvc uid, failed to create data pvc' && exit 1
done

exec 1>&3 3>&- 

printf "$uid"