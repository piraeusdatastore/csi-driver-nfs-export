#!/bin/sh -a

exec 3>&1 1>&2

nfs_sts="$1"

envsubst < tmpl/nfs-svc.yaml | kubectl apply -f -

SECONDS=0
cluster_ip=
while [ -z "$cluster_ip" ] ; do
    cluster_ip="$( kubectl -n volume-nfs get svc "$nfs_sts" -o jsonpath='{.spec.clusterIP}' )"
    sleep 2
    [ "$SECONDS" -ge 30 ] && echo 'Cannot get cluster ip, failed to create nfs svc' && exit 1
done

exec 1>&3 3>&- 

printf "$cluster_ip"