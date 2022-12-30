# CSI NFS Export Driver (EXPERIMENTAL)

> **Note:**
> 
> This repo is in PoC stage

## Overview

1. This goal is to provide a "per-volume" `NFS export` for `PersistentVolumes`, both dynamically and statically.

1. Use an NFS-Ganesha `Pod` to export NFS

1. Use `ClusterIP` to mount NFS

1. Use CSI NFS Mounter instead of intree NFS volume

## Data Flow

![Data Plane](img/data_flow.drawio.svg)