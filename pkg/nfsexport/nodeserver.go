/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package nfsexport

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"encoding/json"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/volume"
	mount "k8s.io/mount-utils"

	"time"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"github.com/hexops/valast"
)

// NodeServer driver
type NodeServer struct {
	Driver  *Driver
	mounter mount.Interface
}

// NodePublishVolume mount the volume
func (ns *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	targetPath := req.GetTargetPath()
	klog.V(2).Infof("Target Path is : %s", targetPath)
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path not provided")
	}


	mountOptions := volCap.GetMount().GetMountFlags()
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	var backendPvcName, backendNs, backendImg string
	subDirReplaceMap := map[string]string{}

	bs, _ := json.Marshal(req.GetVolumeContext())
    klog.V(2).Infof("VolumeContext: %s", string(bs))

	mountPermissions := ns.Driver.mountPermissions
	for k, v := range req.GetVolumeContext() {
		switch strings.ToLower(k) {
		case paramBackendVolumeClaim:
			backendPvcName = v
		case paramBackendNamespace:
			backendNs = v
		case paramBackendPodImage:
			backendImg = v
		case pvcNamespaceKey:
			subDirReplaceMap[pvcNamespaceMetadata] = v
		case pvcNameKey:
			subDirReplaceMap[pvcNameMetadata] = v
		case pvNameKey:
			subDirReplaceMap[pvNameMetadata] = v
		case mountOptionsField:
			if v != "" {
				mountOptions = append(mountOptions, v)
			}
		case mountPermissionsField:
			if v != "" {
				var err error
				if mountPermissions, err = strconv.ParseUint(v, 8, 32); err != nil {
					return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("invalid mountPermissions %s", v))
				}
			}
		}
	}


	if backendPvcName == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("%v is a required parameter", paramBackendVolumeClaim))
	}
	if backendNs == "" {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("%v is a required parameter", paramBackendNamespace))
	}

	// Check if backend PVC exists
	backendPvc, err := ns.Driver.clientSet.CoreV1().PersistentVolumeClaims(backendNs).Get(context.TODO(), backendPvcName, metav1.GetOptions{})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	backendPvcUid := backendPvc.ObjectMeta.UID

	// Create backend Service
	backendSvcName := backendPvcName
	klog.V(2).Infof("Backend Service \"%s\"", backendSvcName )
	backendSvcDef := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: backendSvcName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "PersistentVolumeClaim",
					Name:               backendPvcName,
					UID:                backendPvcUid,
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
					"nfs-export.csi.k8s.io/backend-pvc": backendPvcName,
				},
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:		"nfs",
					Protocol:	corev1.ProtocolTCP,
					Port:		2049,
				},
				{
					Name:		"rpc-tcp",
					Protocol:	corev1.ProtocolTCP,
					Port:		111,
				},
				{
					Name:		"rpc-udp",
					Protocol:	corev1.ProtocolUDP,
					Port:		111,
				},
			},
		},
	}

	_, err = ns.Driver.clientSet.CoreV1().Services(backendNs).Get(context.TODO(), backendSvcName, metav1.GetOptions{})
	if err != nil {
		_, err := ns.Driver.clientSet.CoreV1().Services(backendNs).Create(context.TODO(), backendSvcDef, metav1.CreateOptions{})
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}

	backendClusterIp := ""
	for {
		time.Sleep(1 * time.Second)
		klog.V(2).Infof("Wait for backend service to be ready: %s", backendSvcName)
		backendSvc, err := ns.Driver.clientSet.CoreV1().Services(backendNs).Get(context.TODO(), backendSvcName, metav1.GetOptions{})
		if err == nil {
			backendClusterIp = backendSvc.Spec.ClusterIP;
			klog.V(2).Infof("Backend IP is \"%s\"", backendClusterIp)
		} else {
			klog.V(2).Infof("Waiting for Backend Service to get ready: \"%s\"", backendSvcName)
		}
		if backendClusterIp != "" {
			break
		} 
	}

	// Create backend Pod to connect backend SVC with backend PVC
	backendPodName := backendPvcName
	klog.Infof("Creating backend POD: \"%s\"", backendPodName)

	backendPodDef := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: backendPodName,
			Labels: map[string]string{
				"nfs-export.csi.k8s.io/backend-pvc": backendPvcName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "PersistentVolumeClaim",
					Name:               backendPvcName,
					UID:                backendPvcUid,
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "export",
					Image: backendImg,
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						Privileged: valast.Addr(true).(*bool),
					},
					// SecurityContext: &corev1.SecurityContext{
					// 	Capabilities: &corev1.Capabilities{
					// 		Add: []corev1.Capability{
					// 			"SYS_ADMIN",
					// 			"SETPCAP",
					// 			"DAC_READ_SEARCH",
					// 		},
					// 	},
					// },
					Args: []string{
						"/export",
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "nfs",
							Protocol:      corev1.ProtocolTCP,
							ContainerPort: 2049,
						},
						{
							Name:          "rpc-tcp",
							Protocol:      corev1.ProtocolTCP,
							ContainerPort: 111,
						},
						{
							Name:          "rpc-udp",
							Protocol:      corev1.ProtocolUDP,
							ContainerPort: 111,
						},
					},
					ReadinessProbe: &corev1.Probe {
						ProbeHandler: corev1.ProbeHandler{
							TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromString("nfs"),
							},
						},
						InitialDelaySeconds: 1,
						PeriodSeconds: 1,
						SuccessThreshold: 3,
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:			  "share",
							MountPath:		  "/share",
							MountPropagation: valast.Addr(corev1.MountPropagationMode("Bidirectional")).(*corev1.MountPropagationMode),
						},
						{
							Name:	   "export",
							MountPath: "/export",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "share", 
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
				{
					Name: "export",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: backendPvcName,
						},
					},
				},
			},
		},
	}

	_, err = ns.Driver.clientSet.CoreV1().Pods(backendNs).Get(context.TODO(), backendPodName, metav1.GetOptions{})
	if err != nil {
		_, err = ns.Driver.clientSet.CoreV1().Pods(backendNs).Create(context.TODO(), backendPodDef, metav1.CreateOptions{})
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}

	// Wait for Pod to be ready
	for {
		time.Sleep(1 * time.Second)
		klog.V(2).Infof("Wait for backend POD to be ready: %s", backendPodName)
		backendPod, err := ns.Driver.clientSet.CoreV1().Pods(backendNs).Get(context.TODO(), backendPodName, metav1.GetOptions{})
		if err == nil && backendPod.Status.Phase == corev1.PodRunning {
			break
		} 
	}

	// Mount nfs export
	server  := backendClusterIp
	baseDir := "/"

	server = getServerFromSource(server)
	source := fmt.Sprintf("%s:%s", server, baseDir)

	notMnt, err := ns.mounter.IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(targetPath, os.FileMode(mountPermissions)); err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			notMnt = true
		} else {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	if !notMnt {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	klog.V(2).Infof("NodePublishVolume: volumeID(%v) source(%s) targetPath(%s) mountflags(%v)", volumeID, source, targetPath, mountOptions)

	localSource := "/var/snap/microk8s/common/default-storage/local" // experimental
	if localSource != "" {
		err = ns.mounter.Mount(localSource, targetPath, "", []string{"bind"})
	} else {
		err = ns.mounter.Mount(source, targetPath, "nfs", mountOptions)
	}
	
	if err != nil {
		if os.IsPermission(err) {
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		if strings.Contains(err.Error(), "invalid argument") {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	if mountPermissions > 0 {
		if err := chmodIfPermissionMismatch(targetPath, os.FileMode(mountPermissions)); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	} else {
		klog.V(2).Infof("skip chmod on targetPath(%s) since mountPermissions is set as 0", targetPath)
	}
	klog.V(2).Infof("volume(%s) mount %s on %s succeeded", volumeID, source, targetPath)
	
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume unmount the volume
func (ns *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	klog.V(2).Infof("NodeUnpublishVolume: unmounting volume %s on %s", volumeID, targetPath)
	err := mount.CleanupMountPoint(targetPath, ns.mounter, true /*extensiveMountPointCheck*/)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to unmount target %q: %v", targetPath, err)
	}
	klog.V(2).Infof("NodeUnpublishVolume: unmount volume %s on %s successfully", volumeID, targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetInfo return info of the node on which this plugin is running
func (ns *NodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	return &csi.NodeGetInfoResponse{
		NodeId: ns.Driver.nodeID,
	}, nil
}

// NodeGetCapabilities return the capabilities of the Node plugin
func (ns *NodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: ns.Driver.nscap,
	}, nil
}

// NodeGetVolumeStats get volume stats
func (ns *NodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if len(req.VolumeId) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats volume ID was empty")
	}
	if len(req.VolumePath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats volume path was empty")
	}

	if _, err := os.Lstat(req.VolumePath); err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "path %s does not exist", req.VolumePath)
		}
		return nil, status.Errorf(codes.Internal, "failed to stat file %s: %v", req.VolumePath, err)
	}

	volumeMetrics, err := volume.NewMetricsStatFS(req.VolumePath).GetMetrics()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get metrics: %v", err)
	}

	available, ok := volumeMetrics.Available.AsInt64()
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to transform volume available size(%v)", volumeMetrics.Available)
	}
	capacity, ok := volumeMetrics.Capacity.AsInt64()
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to transform volume capacity size(%v)", volumeMetrics.Capacity)
	}
	used, ok := volumeMetrics.Used.AsInt64()
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to transform volume used size(%v)", volumeMetrics.Used)
	}

	inodesFree, ok := volumeMetrics.InodesFree.AsInt64()
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to transform disk inodes free(%v)", volumeMetrics.InodesFree)
	}
	inodes, ok := volumeMetrics.Inodes.AsInt64()
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to transform disk inodes(%v)", volumeMetrics.Inodes)
	}
	inodesUsed, ok := volumeMetrics.InodesUsed.AsInt64()
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to transform disk inodes used(%v)", volumeMetrics.InodesUsed)
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Unit:      csi.VolumeUsage_BYTES,
				Available: available,
				Total:     capacity,
				Used:      used,
			},
			{
				Unit:      csi.VolumeUsage_INODES,
				Available: inodesFree,
				Total:     inodes,
				Used:      inodesUsed,
			},
		},
	}, nil
}

// NodeUnstageVolume unstage volume
func (ns *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeStageVolume stage volume
func (ns *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeExpandVolume node expand volume
func (ns *NodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func makeDir(pathname string) error {
	err := os.MkdirAll(pathname, os.FileMode(0755))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}
