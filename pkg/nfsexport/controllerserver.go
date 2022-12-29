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

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"k8s.io/klog/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	// "k8s.io/client-go/kubernetes"
)

// ControllerServer controller server setting
type ControllerServer struct {
	Driver *Driver
}

type backendPvc struct {
	name string
	namespace string
	size int64
	StorageClass string
	image string
}

const separator = "#"

// CreateVolume create a volume
func (cs *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	frontendPvName := req.GetName()
	if len(frontendPvName) == 0 {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume name must be provided")
	}

	if err := isValidVolumeCapabilities(req.GetVolumeCapabilities()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// mountPermissions := cs.Driver.mountPermissions
	size := req.GetCapacityRange().GetRequiredBytes()
	
	parameters := req.GetParameters()
	if parameters == nil {
		parameters = make(map[string]string)
	}

    var backendSc, backendImg, backendNs string

	// validate parameters (case-insensitive)
	for k, v := range parameters {
		switch strings.ToLower(k) {
		case paramBackendStorageClass:
			backendSc = v
		case paramBackendPodImage:
			backendImg = v
		case paramBackendNamespace:
			backendNs = v
		case pvcNamespaceKey:
		case pvcNameKey:
		case pvNameKey:
			// no op
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

	// Create BackendPvc
	backendPvcName := "backend-" + frontendPvName

	klog.V(2).Infof("Backend StorageClass is: %s", backendSc)
	klog.V(2).Infof("Backend Pod Image is: %s", backendImg)
	klog.V(2).Infof("Backend Namespace is: %s", backendNs)
	klog.V(2).Infof("Backend PVC Name is: %s", backendPvcName )

	resourceStorage := resource.NewQuantity(size, resource.BinarySI)

	backendPvcDef := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: backendPvcName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &backendSc,
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

	backendPvc, err := cs.Driver.clientSet.CoreV1().PersistentVolumeClaims(backendNs).Get(context.TODO(), backendPvcName, metav1.GetOptions{})
	if err != nil { // check if backend PVC already exists. Needs a more strict verification here
		backendPvc, err = cs.Driver.clientSet.CoreV1().PersistentVolumeClaims(backendNs).Create(context.TODO(),backendPvcDef, metav1.CreateOptions{})
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	
    backendPvcUid := string(backendPvc.ObjectMeta.UID)
	klog.V(2).Infof("Backend PVC uid is: %s", backendPvcUid )

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      backendPvcUid, // CSI Volume Handle, needs improvement here
			CapacityBytes: 0, // by setting it to zero, Provisioner will use PVC requested size as PV size
			VolumeContext: map[string]string{
				"backendVolumeClaim"  	: backendPvcName,
				"backendNamespace" 	  	: backendNs,
				"backendStorageClass" 	: backendSc,
				"backendPodImage"     	: backendImg,
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
	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *ControllerServer) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}

func (cs *ControllerServer) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
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
	for _, c := range volCaps {
		if c.GetBlock() != nil {
			return fmt.Errorf("block volume capability not supported")
		}
	}
	return nil
}