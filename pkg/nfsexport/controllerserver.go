/*
Copyright 2020 The Kubernetes Authors.

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
	// "os"
	//"path/filepath"
	//"regexp"
	"strings"
	//"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/klog/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"github.com/google/uuid"
)

// ControllerServer controller server setting
type ControllerServer struct {
	Driver *Driver
}

// CreateVolume create a volume
func (cs *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	nfsPVName := req.GetName()
	if len(nfsPVName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume name must be provided")
	}

	if err := isValidVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	
	parameters := req.GetParameters()
	if parameters == nil {
		parameters = make(map[string]string)
	}

    var dataSC, nfsServerImage, nfsHostsAllow, nfsPVCName, nfsPVCNamespace string
	// validate parameters (case-insensitive)
	// mountPermissions := cs.Driver.mountPermissions
	for k, v := range parameters {
		switch strings.ToLower(k) {
		case paramDataStorageClass:
			dataSC = v
		case paramNfsServerImage:
			nfsServerImage = v
		case paramNfsHostsAllow:
			nfsHostsAllow = v
		case pvcNameKey:
			nfsPVCName = v
		case pvcNamespaceKey:
			nfsPVCNamespace = v
		case pvNameKey:
			klog.V(2).Infof("pvNamekey: %s", v)
		case mountPermissionsField:
			// if v != "" {
			// 	var err error
			// 	if mountPermissions, err = strconv.ParseUint(v, 8, 32); err != nil {
			// 		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("invalid mountPermissions %s in storage class", v))
			// 	}
			// }
		default:
			return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("invalid parameter %q in storage class", k))
		}
	}

	// Create BackendPVC
	volumeID := "nfs-" + uuid.New().String()
	dataPVCName := volumeID
	dataNamespace := nfsPVCNamespace

	if dataSC == "" { // use default StorageClass if none provided 
		scList, err := cs.Driver.clientSet.StorageV1().StorageClasses().List(context.TODO(), metav1.ListOptions{})
		if err == nil {
			for _, sc := range scList.Items {
				if a := sc.ObjectMeta.Annotations; a != nil {
					if v, ok := a["storageclass.kubernetes.io/is-default-class"]; ok && v == "true" {
						dataSC = sc.ObjectMeta.Name
						break
					}
				}
			}
		}
	}

	if nfsServerImage == "" {
		nfsServerImage = "daocloud.io/piraeus/nfs-ganesha:latest" // default image
	}

	klog.V(2).Infof("NFS PVC Name is: %s", nfsPVCName)
	klog.V(2).Infof("NFS PVC Namespace is: %s", nfsPVCNamespace)
	klog.V(2).Infof("NFS Pod Image is: %s", nfsServerImage)
	klog.V(2).Infof("NFS Allowed Host IPs are: %s", nfsHostsAllow )
	klog.V(2).Infof("DATA StorageClass is: %s", dataSC)
	klog.V(2).Infof("DATA Namespace is: %s", dataNamespace)
	klog.V(2).Infof("DATA PVC Name is: %s", dataPVCName )

	size := req.GetCapacityRange().GetRequiredBytes()
	resourceStorage := resource.NewQuantity(size, resource.BinarySI)

	dataPVCDef := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: dataPVCName,
		Labels: map[string]string{
				"nfs-export.csi.k8s.io/id": volumeID,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &dataSC,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: *resourceStorage,
				},
			},
		},
	}

	dataPVC, err := cs.Driver.clientSet.CoreV1().PersistentVolumeClaims(dataNamespace).Get(context.TODO(), dataPVCName, metav1.GetOptions{})
	if err != nil { // check if nfs PVC already exists. Needs a more strict verification here
		dataPVC, err = cs.Driver.clientSet.CoreV1().PersistentVolumeClaims(dataNamespace).Create(context.TODO(),dataPVCDef, metav1.CreateOptions{})
		if err != nil {
			return nil, status.Error(codes.Canceled, err.Error())
		}
	}
    dataPVCUID := dataPVC.ObjectMeta.UID
	klog.V(2).Infof("Backend PVC uid is: %s", dataPVCUID)

	// nfs PVC, POD and SVC all use the same name
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volumeID, // CSI Volume Handle, needs improvement here
			CapacityBytes: 0, // by setting it to zero, Provisioner will use PVC requested size as PV size
			VolumeContext: map[string]string{
				paramDataVolumeClaim : dataPVCName,
				paramDataNamespace	 : dataNamespace,
				paramNfsServerImage	 : nfsServerImage,
				paramNfsHostsAllow   : nfsHostsAllow,
			},
		},
	}, nil
}

// DeleteVolume delete a volume
func (cs *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume id is empty")
	}

	nfsPV, err := getPvById(cs.Driver.clientSet, volumeID)
	if err != nil {
		klog.V(2).Infof("Cannot find nfs PV by ID: %s", volumeID )
		return &csi.DeleteVolumeResponse{}, nil
	}

	dataPVCName := nfsPV.Spec.PersistentVolumeSource.CSI.VolumeAttributes["nfsVolumeClaim"]
	dataNamespace := nfsPV.Spec.PersistentVolumeSource.CSI.VolumeAttributes["nfsNamespace"]
	klog.V(2).Infof("Data PVC Name is: %s", dataPVCName)
	klog.V(2).Infof("Data PVC Namespace is: %s", dataNamespace )
	err = cs.Driver.clientSet.CoreV1().PersistentVolumeClaims(dataNamespace).Delete(context.TODO(), dataPVCName, metav1.DeleteOptions{})
	if err != nil {
		klog.V(2).Infof("Cannot delete NFS PVC: ", err )
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	// klog.V(2).Infof("Controller Publish is here!" )
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	/// klog.V(2).Infof("Controller Unpublish is here!" )
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if err := isValidVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: req.GetVolumeCapabilities(),
		},
		Message: "",
	}, nil
}

func (cs *ControllerServer) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// ControllerGetCapabilities implements the default GRPC callout.
// Default supports all capabilities
func (cs *ControllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: cs.Driver.cscap,
	}, nil
}

func (cs *ControllerServer) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

// isValidVolumeCapabilities validates the given VolumeCapability array is valid
func isValidVolumeCapabilities(volCaps []*csi.VolumeCapability) error {
	if len(volCaps) == 0 {
		return fmt.Errorf("volume capabilities missing in request")
	}
	return nil
}