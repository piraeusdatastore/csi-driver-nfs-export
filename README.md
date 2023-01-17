# CSI NFS Export Driver (EXPERIMENTAL)

> **Note:**
> This repo is in PoC stage

## Overview

1. This goal is to provide a "per-volume" `NFS export` for `PersistentVolumes`, both dynamically and statically.

1. Use an NFS-Ganesha `Pod` to export NFS

1. Use `ClusterIP` to mount NFS

1. Use CSI NFS Mounter instead of intree NFS volume

## Data Flow

![Data Plane](img/data_flow.drawio.svg)

## Highlights

1. Support Static Provisioning
2. Bind mount the local path on the data node
3. Implement controller.kubernetes.io/pod-deletion-cost
4. Delete NFS Pod and NFS SVC when NFS PVC is completely umounted
5. Implement NFS Clients Filter for NFS security (Ganesha only)

## How to deploy

```Console
$ kubectl apply -f deploy/

$ kubectl get pod -l nfs-export.csi.k8s.io/server
NAME                                        READY   STATUS    RESTARTS   AGE
csi-nfs-export-controller-79f987457-t8rzt   3/3     Running   0          16m
csi-nfs-export-node-5h5cj                   3/3     Running   0          16m
csi-nfs-export-node-mgldw                   3/3     Running   0          16m
csi-nfs-export-node-272q7                   3/3     Running   0          16m
```
