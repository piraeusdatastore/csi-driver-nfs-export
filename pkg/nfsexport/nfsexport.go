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
	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog/v2"
	mount "k8s.io/mount-utils"
	"k8s.io/client-go/kubernetes"
)

// DriverOptions defines driver parameters specified in driver deployment
type DriverOptions struct {
	NodeID           string
	DriverName       string
	Endpoint         string
	MountPermissions uint64
	WorkingMountDir  string
}

type Driver struct {
	name             string
	nodeID           string
	version          string
	endpoint         string
	mountPermissions uint64
	workingMountDir  string

	//ids *identityServer
	ns          *NodeServer
	cscap       []*csi.ControllerServiceCapability
	nscap       []*csi.NodeServiceCapability
	volumeLocks *VolumeLocks

	// kuberntes clientset
	clientSet *kubernetes.Clientset
}

const (
	paramServer	= "server"
	paramShare  = "share"
	paramSubDir = "subdir"

	DefaultDriverName         = "nfs-export.csi.k8s.io"
	paramDataVolumeClaim	  = "datavolumeclaim"
	paramDataStorageClass     = "datastorageclass"
	paramDataNamespace     	  = "datanamespace"
	paramNfsServerImage 	  = "nfsserverimage"
	paramNfsHostsAllow	      = "nfshostsallow"

	mountOptionsField         = "mountoptions"
	mountPermissionsField 	  = "mountpermissions"

	pvcNameKey                = "csi.storage.k8s.io/pvc/name"
	pvcNamespaceKey           = "csi.storage.k8s.io/pvc/namespace"
	pvNameKey                 = "csi.storage.k8s.io/pv/name"

	pvcNameMetadata           = "${pvc.metadata.name}"
	pvcNamespaceMetadata      = "${pvc.metadata.namespace}"
	pvNameMetadata            = "${pv.metadata.name}"

	podNameKey				  = "csi.storage.k8s.io/pod.name"
	podNamespaceKey			  = "csi.storage.k8s.io/pod.namespace"
)

func NewDriver(options *DriverOptions, clientset *kubernetes.Clientset) *Driver {
	klog.V(2).Infof("Driver: %v version: %v", options.DriverName, driverVersion)

	n := &Driver{
		name:             options.DriverName,
		version:          driverVersion,
		nodeID:           options.NodeID,
		endpoint:         options.Endpoint,
		mountPermissions: options.MountPermissions,
		workingMountDir:  options.WorkingMountDir,

		clientSet: 		  clientset,
	}

	n.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
		// csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
		// csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME, // require volumeattachments
	})

	n.AddNodeServiceCapabilities([]csi.NodeServiceCapability_RPC_Type{
		// csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
		csi.NodeServiceCapability_RPC_UNKNOWN,
	})
	n.volumeLocks = NewVolumeLocks()
	return n
}

func NewNodeServer(n *Driver, mounter mount.Interface) *NodeServer {
	return &NodeServer{
		Driver:  n,
		mounter: mounter,
	}
}

func (n *Driver) Run(testMode bool) {
	versionMeta, err := GetVersionYAML(n.name)
	if err != nil {
		klog.Fatalf("%v", err)
	}
	klog.V(2).Infof("\nDRIVER INFORMATION:\n-------------------\n%s\n\nStreaming logs below:", versionMeta)

	n.ns = NewNodeServer(n, mount.New(""))
	s := NewNonBlockingGRPCServer()
	s.Start(n.endpoint,
		NewDefaultIdentityServer(n),
		// NFS plugin has not implemented ControllerServer
		// using default controllerserver.
		NewControllerServer(n),
		n.ns,
		testMode)
	s.Wait()
}

func (n *Driver) AddControllerServiceCapabilities(cl []csi.ControllerServiceCapability_RPC_Type) {
	var csc []*csi.ControllerServiceCapability
	for _, c := range cl {
		csc = append(csc, NewControllerServiceCapability(c))
	}
	n.cscap = csc
}

func (n *Driver) AddNodeServiceCapabilities(nl []csi.NodeServiceCapability_RPC_Type) {
	var nsc []*csi.NodeServiceCapability
	for _, n := range nl {
		nsc = append(nsc, NewNodeServiceCapability(n))
	}
	n.nscap = nsc
}