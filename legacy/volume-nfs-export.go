package nfsexport

import (
	"strconv"
	"time"
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog"
)

// func int32Ptr(i int32) *int32 { return &i }

var vecRes = schema.GroupVersionResource{
	Group: "nfsexport.rafflescity.io", 
	Version: "v1alpha1", 
	Resource: "volumeexportcontents",
}

var veRes = schema.GroupVersionResource{
	Group: "nfsexport.rafflescity.io", 
	Version: "v1alpha1", 
	Resource: "volumeexports",
}

type volumeNfsExport struct {
	FrontendPvcNs	 	string
	FrontendPvcName	 	string
	FrontendPvName		string

	BackendScName 		string

	BackendPvcNs 		string
	BackendPvcName 		string
	BackendPvName		string

	BackendPodName 		string
	NfsExporterImage 	string

	BackendSvcName		string
	BackendClusterIp    string

	Capacity			resource.Quantity
	LogID				string
}

// NewVolumeNfsExport creates a new volumeNfsExport
func CreateVolumeNfsExport(v *volumeNfsExport, cs *kubernetes.Clientset, dcs dynamic.Interface) *volumeNfsExport  {
	klog.Infof( v.LogID + "Backend SC is \"%s\"", v.BackendScName )
	klog.Infof( v.LogID + "NFS Exporter Image is \"%s\"", v.NfsExporterImage )

	if v.BackendClusterIp != "" {
		return v
	}

	// Create backend PVC
	klog.Infof(v.LogID + "Creating backend PVC \"%s\"", v.BackendPvcName )
	size := strconv.FormatInt( v.Capacity.Value(), 10 )
	klog.Infof( v.LogID + "Backend PVC size is \"%s\"", size )

	backendPvcDef := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: v.BackendPvcName,
			Labels: map[string]string{
				"nfsexport.rafflescity.io/frontend-pod": v.BackendPodName,
				"nfsexport.rafflescity.io/frontend-pvc": v.FrontendPvcName,
				"nfsexport.rafflescity.io/frontend-pvc-namespace": v.FrontendPvcNs,
				"nfsexport.rafflescity.io/frontend-pv": v.FrontendPvName,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &v.BackendScName,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): v.Capacity,
				},
			},
		},
	}

    backendPvc, err := cs.CoreV1().PersistentVolumeClaims(v.BackendPvcNs).Create(context.TODO(),backendPvcDef, metav1.CreateOptions{})
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
			Labels: map[string]string{
				"nfsexport.rafflescity.io/frontend-pvc": v.FrontendPvcName,
				"nfsexport.rafflescity.io/frontend-pvc-namespace": v.FrontendPvcNs,
				"nfsexport.rafflescity.io/frontend-pv": v.FrontendPvName,
			},
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

	_, err = cs.CoreV1().Services(v.BackendPvcNs).Create(context.TODO(), backendSvcDef, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	v.BackendClusterIp = ""
	for {
		time.Sleep(1 * time.Second)
		backendSvc, err := cs.CoreV1().Services(v.BackendPvcNs).Get(context.TODO(), v.BackendSvcName, metav1.GetOptions{})
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
			Labels: map[string]string{
				"nfsexport.rafflescity.io/frontend-pvc": v.FrontendPvcName,
				"nfsexport.rafflescity.io/frontend-pvc-namespace": v.FrontendPvcNs,
				"nfsexport.rafflescity.io/frontend-pv": v.FrontendPvName,
			},
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
					Image: v.NfsExporterImage,
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

	_, err = cs.CoreV1().Pods(v.BackendPvcNs).Create(context.TODO(), backendPodDef, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	// Wait until NFS Pod is ready
	backendPodStatus := corev1.PodUnknown
	for {
		time.Sleep(1 * time.Second)
		backendPod, err := cs.CoreV1().Pods(v.BackendPvcNs).Get(context.TODO(), v.BackendPodName, metav1.GetOptions{})
		if err == nil {
			backendPodStatus = backendPod.Status.Phase;
			klog.Infof( v.LogID + "Frontend Pod status is: \"%s\"", backendPodStatus )
		} else {
			klog.Infof( v.LogID + "Waiting for frontend Pod to spawn: \"%s\"", v.BackendPodName )
		}
		if backendPodStatus == corev1.PodRunning {
			break
		}
	}

		// Create CRD volumeExport
		veDef := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "nfsexport.rafflescity.io/v1alpha1",
				"kind":       "VolumeExport",
				"metadata": map[string]interface{}{
					"name": v.FrontendPvName,
					"namespace": v.FrontendPvcNs,
				},
				"spec": map[string]interface{}{
					"backendVolumeClaim":	v.BackendPvcNs + "/" + v.BackendPvcName,
					"nfsExport":			v.BackendClusterIp + ":/",
				},
			},
		}
		_, err = dcs.Resource(veRes).Namespace(v.BackendPvcNs).Create(context.TODO(), veDef, metav1.CreateOptions{})
		klog.Infof( v.LogID + "Creating CRD VolumeExport: \"%s\"", err )

	// Create CRD volumeExportContent
	vecDef := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "nfsexport.rafflescity.io/v1alpha1",
			"kind":       "VolumeExportContent",
			"metadata": map[string]interface{}{
				"name": v.FrontendPvName,
			},
			"spec": map[string]interface{}{
				"backendVolume": v.BackendPvName,
			},
		},
	}
	_, err = dcs.Resource(vecRes).Create(context.TODO(), vecDef, metav1.CreateOptions{})
	klog.Infof( v.LogID + "Creating CRD VolumeExportContent: \"%s\"", err )


	return v
}
