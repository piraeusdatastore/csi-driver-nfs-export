#!/bin/sh

exec 1>&2

nfs_pv_name="$1"
nfs_sts_name="$nfs_pv_name"
nfs_svc_name="$nfs_sts_name"
data_pvc_name="$( echo "$nfs_sts_name" | sed 's/^pvc-/data-/; s/$/-0/' )"

kubectl -n volume-nfs delete sts "$nfs_sts_name"

kubectl -n volume-nfs delete svc "$nfs_svc_name"

kubectl -n volume-nfs delete pvc "$data_pvc_name"