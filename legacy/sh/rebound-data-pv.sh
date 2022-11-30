#!/bin/sh -x
nfs_pvc_ns="$1"
nfs_pvc_name="$2"
nfs_sts_name="$3"
data_pvc_name="$4"
data_pv_name="$5"

exec 1>&2 

kubectl -n volume-nfs delete sts "$nfs_sts_name"

kubectl patch pv "$data_pv_name" -p '{"spec":{"persistentVolumeReclaimPolicy":"Retain"}}'

kubectl -n volume-nfs delete pvc "$data_pvc_name"

kubectl patch pv "$data_pv_name" -p "{\"spec\":{\"claimRef\":{\"apiVersion\":\"v1\",\"kind\":\"PersistentVolumeClaim\",\"name\":\"${nfs_pvc_name}\",\"namespace\":\"${nfs_pvc_ns}\",\"uid\":\"\",\"resourceVersion\":\"\"}}}"

sleep 5