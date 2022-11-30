package nfsexport

import (
	"time"
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

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

func newNfsExportVolume(frontendPvName string, size int64, params map[string]string, c *kubernetes.Clientset) (*nfsVolume, error) {

	vneDef := &nfsExport{
		FrontendPvcNs:	 	"",
		FrontendPvcName:	"",
		FrontendPvName:		frontendPvName,
	
		BackendScName: 		params["backendStorageClass"],
	
		BackendNs: 		    "csi-nfs-export",
		BackendPvcName: 	frontendPvName + "-backend",
		BackendPvName:		"",

		BackendPodName: 	frontendPvName + "-backend",
		ExporterImage: 		params["nfsExporterImage"],
	
		BackendSvcName:		frontendPvName + "-backend",
		BackendClusterIp:   "",

		Size:				size,
		LogID:				"["  + "/"  + "] ",
	}



	vne := createNfsExport(vneDef, c)


	vol := &nfsVolume{
		server:  vne.BackendClusterIp,
		baseDir: "/",
		size:    size,
	}
	vol.id = getVolumeIDFromNfsVol(vol)
	return vol, nil
}


func createNfsExport(v *nfsExport, c *kubernetes.Clientset) *nfsExport  {
	klog.Infof( v.LogID + "Backend SC is \"%s\"", v.BackendScName )
	klog.Infof( v.LogID + "NFS Exporter Image is \"%s\"", v.ExporterImage )

	if v.BackendClusterIp != "" {
		return v
	}

	// Create backend PVC
	klog.Infof(v.LogID + "Creating backend PVC \"%s\"", v.BackendPvcName )
	klog.Infof( v.LogID + "Backend PVC size is \"%d\"", v.Size )

	resourceStorage := resource.NewQuantity(v.Size, resource.BinarySI)

	backendPvcDef := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: v.BackendPvcName,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &v.BackendScName,
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

    backendPvc, err := c.CoreV1().PersistentVolumeClaims(v.BackendNs).Create(context.TODO(),backendPvcDef, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	backendPvcUid := backendPvc.ObjectMeta.UID
	klog.Infof(v.LogID + "Backend PVC uid is \"%s\"", backendPvcUid )
	v.BackendPvName = "pvc-" + string( backendPvcUid )

	// Create frontend SVC
	klog.Infof(v.LogID + "Frontend SVC \"%s\"", v.BackendSvcName )
	backendSvcDef := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: v.BackendSvcName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "PersistentVolumeClaim",
					Name:               v.BackendPvcName,
					UID:                backendPvcUid,
				},
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
					"nfsexport.rafflescity.io/frontend-pv": v.FrontendPvName,
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

	_, err = c.CoreV1().Services(v.BackendNs).Create(context.TODO(), backendSvcDef, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	v.BackendClusterIp = ""
	for {
		time.Sleep(1 * time.Second)
		backendSvc, err := c.CoreV1().Services(v.BackendNs).Get(context.TODO(), v.BackendSvcName, metav1.GetOptions{})
		if err == nil {
			v.BackendClusterIp = backendSvc.Spec.ClusterIP;
			klog.Infof(v.LogID + "Frontend IP is \"%s\"", v.BackendClusterIp)
		} else {
			klog.Infof(v.LogID + "Waiting for NFS SVC to spawn: \"%s\"", v.BackendSvcName)
		}
		if v.BackendClusterIp != "" {
			break
		} 
	}

	// Create frontend Pod to connect frontend SVC with backend PVC
	klog.Infof(v.LogID + "Creating frontend pod: \"%s\"", v.BackendPodName)

	backendPodDef := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: v.BackendPodName,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "PersistentVolumeClaim",
					Name:               v.BackendPvcName,
					UID:                backendPvcUid,
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "export",
					Image: v.ExporterImage,
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
							ClaimName: v.BackendPvcName,
						},
					},
				},
			},
		},
	}

	_, err = c.CoreV1().Pods(v.BackendNs).Create(context.TODO(), backendPodDef, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	// Wait until NFS Pod is ready
	// backendPodStatus := corev1.PodUnknown
	// for {
	// 	time.Sleep(1 * time.Second)
	// 	backendPod, err := c.CoreV1().Pods(v.BackendNs).Get(context.TODO(), v.BackendPodName, metav1.GetOptions{})
	// 	if err == nil {
	// 		backendPodStatus = backendPod.Status.Phase;
	// 		klog.Infof( v.LogID + "Frontend Pod status is: \"%s\"", backendPodStatus )
	// 	} else {
	// 		klog.Infof( v.LogID + "Waiting for frontend Pod to spawn: \"%s\"", v.BackendPodName )
	// 	}
	// 	if backendPodStatus == corev1.PodRunning {
	// 		break
	// 	}
	// }

	return v
}