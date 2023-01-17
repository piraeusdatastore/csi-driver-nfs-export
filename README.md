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

## Heighlights 

1. Support `Static Provisioning`
1. Mount local path on the backend node
1. Implement `controller.kubernetes.io/pod-deletion-cost`
1. Remove backend when PVC is completely *umounted*
1. Add export IP Range for NFS security