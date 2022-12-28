package nfsexport

import (
	"time"
	"context"
	"strings"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

// nfsVolume is an internal representation of a volume
// created by the provisioner.
type nfsVolume struct {
	// Volume id
	id string
	// Address of the NFS server.
	// Matches paramServer.
	server string
	// Base directory of the NFS server to create volumes under
	// Matches paramShare.
	baseDir string
	// Subdirectory of the NFS server to create volumes under
	subDir string
	// size of volume
	size int64
	// pv name when subDir is not empty
	uuid string
}

// Create NFS Export
type nfsExport struct {
	FrontendPvcNs	 	string
	FrontendPvcName	 	string
	FrontendPvName		string

	BackendScName 		string

	BackendNs 		    string
	BackendPvcName 		string
	BackendPvName		string

	BackendPodName 		string
	ExporterImage 	    string

	BackendSvcName		string
	BackendClusterIp    string

	Size				int64
	LogID				string
}


// newNFSVolume Convert VolumeCreate parameters to an nfsVolume
func newNFSVolume(name string, size int64, parameters map[string]string) (*nfsVolume, error) {
	var server, baseDir, subDir string
	subDirReplaceMap := map[string]string{}

	// validate parameters (case-insensitive)
	for k, v := range parameters {
		switch strings.ToLower(k) {
		case paramServer:
			server = v
		case paramShare:
			baseDir = v
		case paramSubDir:
			subDir = v
		case pvcNamespaceKey:
			subDirReplaceMap[pvcNamespaceMetadata] = v
		case pvcNameKey:
			subDirReplaceMap[pvcNameMetadata] = v
		case pvNameKey:
			subDirReplaceMap[pvNameMetadata] = v
		}
	}

	if server == "" {
		return nil, fmt.Errorf("%v is a required parameter", paramServer)
	}

	vol := &nfsVolume{
		server:  server,
		baseDir: baseDir,
		size:    size,
	}
	if subDir == "" {
		// use pv name by default if not specified
		vol.subDir = name
	} else {
		// replace pv/pvc name namespace metadata in subDir
		vol.subDir = replaceWithMap(subDir, subDirReplaceMap)
		// make volume id unique if subDir is provided
		vol.uuid = name
	}
	vol.id = getVolumeIDFromNfsVol(vol)
	return vol, nil
}


func newNfsExportVolume(e *nfsExport, c *kubernetes.Clientset) (*nfsVolume, error) {

	klog.Infof( e.LogID + "Backend SC is \"%s\"", e.BackendScName )
	klog.Infof( e.LogID + "NFS Exporter Image is \"%s\"", e.ExporterImage )

	// // Create backend PVC
	// klog.Infof(e.LogID + "Creating backend PVC \"%s\"", e.BackendPvcName )
	// klog.Infof( e.LogID + "Backend PVC size is \"%d\"", e.Size )

	// resourceStorage := resource.NewQuantity(e.Size, resource.BinarySI)

	// backendPvcDef := &corev1.PersistentVolumeClaim{
	// 	ObjectMeta: metav1.ObjectMeta{
	// 		Name: e.BackendPvcName,
	// 	},
	// 	Spec: corev1.PersistentVolumeClaimSpec{
	// 		StorageClassName: &e.BackendScName,
	// 		AccessModes: []corev1.PersistentVolumeAccessMode{
	// 			corev1.ReadWriteOnce,
	// 		},
	// 		Resources: corev1.ResourceRequirements{
	// 			Requests: map[corev1.ResourceName]resource.Quantity{
	// 				corev1.ResourceStorage: *resourceStorage,
	// 			},
	// 		},
	// 	},
	// }

    // backendPvc, err := c.CoreV1().PersistentVolumeClaims(e.BackendNs).Create(context.TODO(),backendPvcDef, metav1.CreateOptions{})
	// if err != nil {
	// 	panic(err)
	// }

	// backendPvcUid := backendPvc.ObjectMeta.UID
	// klog.Infof(e.LogID + "Backend PVC uid is \"%s\"", backendPvcUid )
	// e.BackendPvName = "pvc-" + string( backendPvcUid )

	// Create frontend SVC
	klog.Infof(e.LogID + "Frontend SVC \"%s\"", e.BackendSvcName )
	backendSvcDef := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: e.BackendSvcName,
			// OwnerReferences: []metav1.OwnerReference{
			// 	{
			// 		APIVersion:         "v1",
			// 		Kind:               "PersistentVolumeClaim",
			// 		Name:               e.BackendPvcName,
			// 		UID:                backendPvcUid,
			// 	},
			// },
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
					"nfs-export.csi.k8s.io/frontend-pv": e.FrontendPvName,
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

	_, err := c.CoreV1().Services(e.BackendNs).Create(context.TODO(), backendSvcDef, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	e.BackendClusterIp = ""
	for {
		time.Sleep(1 * time.Second)
		backendSvc, err := c.CoreV1().Services(e.BackendNs).Get(context.TODO(), e.BackendSvcName, metav1.GetOptions{})
		if err == nil {
			e.BackendClusterIp = backendSvc.Spec.ClusterIP;
			klog.Infof(e.LogID + "Frontend IP is \"%s\"", e.BackendClusterIp)
		} else {
			klog.Infof(e.LogID + "Waiting for NFS SVC to spawn: \"%s\"", e.BackendSvcName)
		}
		if e.BackendClusterIp != "" {
			break
		} 
	}

	// Create frontend Pod to connect frontend SVC with backend PVC
	klog.Infof(e.LogID + "Creating frontend pod: \"%s\"", e.BackendPodName)

	backendPodDef := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: e.BackendPodName,
			Labels: map[string]string{
				"nfs-export.csi.k8s.io/frontend-pv": e.FrontendPvName,
			},
			// OwnerReferences: []metav1.OwnerReference{
			// 	{
			// 		APIVersion:         "v1",
			// 		Kind:               "PersistentVolumeClaim",
			// 		Name:               e.BackendPvcName,
			// 		UID:                backendPvcUid,
			// 	},
			// },
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "export",
					Image: e.ExporterImage,
					ImagePullPolicy: corev1.PullAlways,
					SecurityContext: &corev1.SecurityContext{
						Capabilities: &corev1.Capabilities{
							Add: []corev1.Capability{
								"SYS_ADMIN",
								"SETPCAP",
								"DAC_READ_SEARCH",
							},
						},
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
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:		"data",
							MountPath:	"/export",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: e.BackendPvcName,
						},
					},
				},
			},
		},
	}

	_, err = c.CoreV1().Pods(e.BackendNs).Create(context.TODO(), backendPodDef, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	vol := &nfsVolume{
		server:  e.BackendClusterIp,
		baseDir: "/",
		size:    e.Size,
	}
	vol.id = getVolumeIDFromNfsVol(vol)
	return vol, nil
}