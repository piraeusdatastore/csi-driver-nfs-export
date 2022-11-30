#!/bin/sh -a
exec 1>&2

nfs_ns="$1"
nfs_sts="$2"
nfs_pvc="$3"
nfs_pv="$4"
data_pvc="$5"
data_pv="$6"
image_tag=${7:ganesha}

envsubst < tmpl/nfs-sts.yaml | kubectl apply -f -

echo "Waiting for NFS pod \"${nfs_sts}-0\" to be ready"
SECONDS=0
endpoint_ip=
while [ -z "$endpoint_ip" ] ; do
    endpoint_ip="$( kubectl -n volume-nfs get ep "$nfs_sts" -o jsonpath='{.subsets[0].addresses[0].ip}' )"
    sleep 1
    kubectl -n volume-nfs get pod "${nfs_sts}-0" | grep -vw ^NAME | awk '{print "NFS pod status: "$3}'
    sleep 1
    [ "$SECONDS" -ge 60 ] && echo 'Cannot get endpoints, please check volume-nfs pod' && break
done

exit 0