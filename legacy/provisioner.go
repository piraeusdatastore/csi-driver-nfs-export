package main

import (
	"flag"
	"syscall"
	"context"
	"os"
	
	"sigs.k8s.io/sig-storage-lib-external-provisioner/v6/controller"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

type volumeNfsProvisioner struct {
	ClientSet *kubernetes.Clientset
	DclientSet dynamic.Interface
}

// NewVolumeNfsProvisioner creates a new provisioner
func NewVolumeNfsProvisioner(cs *kubernetes.Clientset, dcs dynamic.Interface) controller.Provisioner {
	return &volumeNfsProvisioner{
		ClientSet: cs,
		DclientSet: dcs,
	}
}

var _ controller.Provisioner = &volumeNfsProvisioner{}

// Provision creates a storage asset and returns a PV object representing it.
func (p *volumeNfsProvisioner) Provision(ctx context.Context, options controller.ProvisionOptions) (*corev1.PersistentVolume, controller.ProvisioningState, error) {
	capacity := options.PVC.Spec.Resources.Requests[corev1.ResourceName(corev1.ResourceStorage)]
	backendScName := options.StorageClass.Parameters[ "backendStorageClass" ]
	backendNamespaceAggregation := options.StorageClass.Parameters[ "backendNamespaceAggregation" ]
	backendPvcNs := options.StorageClass.Parameters[ "backendNamespace" ]
	logId := "[" + options.PVC.Namespace + "/" + options.PVC.Name + "] "

	if backendNamespaceAggregation == "false" {
		backendPvcNs = options.PVC.Namespace
	} else if backendPvcNs == "" {
		backendPvcNs = "volume-nfs-export"
	}

	vneDef := &volumeNfsExport{
		FrontendPvcNs:	 	options.PVC.Namespace,
		FrontendPvcName:	options.PVC.Name,
		FrontendPvName:		options.PVName,
	
		BackendScName: 		backendScName,
	
		BackendPvcNs: 		backendPvcNs,
		BackendPvcName: 	options.PVName + "-backend",
		BackendPvName:		"",

		BackendPodName: 	options.PVName + "-backend",
		NfsExporterImage: 	options.StorageClass.Parameters["nfsExporterImage"],
	
		BackendSvcName:		options.PVName + "-backend",
		BackendClusterIp:   "",

		Capacity: 			capacity,
		LogID:				logId,
	}

	vne := CreateVolumeNfsExport(vneDef, p.ClientSet, p.DclientSet)
	
	// Create NFS PV (and return it)
	frontendPv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
			Labels: map[string]string{
				"nfsexport.rafflescity.io/backend-pvc-namespace": vne.BackendPvcNs,
				"nfsexport.rafflescity.io/backend-pvc": vne.BackendPvcName,
				"nfsexport.rafflescity.io/backend-Pod": vne.BackendPodName,
				"nfsexport.rafflescity.io/backend-Svc": vne.BackendSvcName,
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeReclaimPolicy: *options.StorageClass.ReclaimPolicy,
			AccessModes:                   options.PVC.Spec.AccessModes,
			Capacity: corev1.ResourceList{
				corev1.ResourceName(corev1.ResourceStorage): capacity,
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Server:   vne.BackendClusterIp,
					Path:     "/",
					ReadOnly: false,
				},
			},
		},
	}
	return frontendPv, controller.ProvisioningFinished, nil
}

// Delete removes the storage asset that was created by Provision represented
// by the given PV.
func (p *volumeNfsProvisioner) Delete(ctx context.Context, volume *corev1.PersistentVolume) error {
	frontendPvName := volume.ObjectMeta.Name
	frontendPvcNs := volume.Spec.ClaimRef.Namespace
	frontendPvcName := volume.Spec.ClaimRef.Name
	
	backendPvcNs := volume.ObjectMeta.Labels["nfsexport.rafflescity.io/backend-pvc-namespace"]
	backendPvcName := volume.ObjectMeta.Labels["nfsexport.rafflescity.io/backend-pvc"]
	backendPodName	:= volume.ObjectMeta.Labels["nfsexport.rafflescity.io/backend-pod"]
	backendSvcName := volume.ObjectMeta.Labels["nfsexport.rafflescity.io/backend-svc"]

	logId := "[" + frontendPvcNs + "/" + frontendPvcName + "] "
	volumeExportName := frontendPvName
	volumeExportContentName := frontendPvName

	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	// Delete frontend pod, frontend svc, and backend PVC
	// klog.Infof("Deleting frontend Pod: \"%s\"", backendPodName )
	// _ = p.ClientSet.CoreV1().Pods(backendPvcNs).Delete(context.TODO(), backendPodName, deleteOptions)
	// klog.Infof("Deleting frontend SVC: \"%s\"", backendSvcName )
	// _ = p.ClientSet.CoreV1().Services(backendPvcNs).Delete(context.TODO(), backendSvcName, deleteOptions)
	klog.Infof( logId + "Deleting backend PVC: \"%s\"", backendPvcName )
	_ = p.ClientSet.CoreV1().PersistentVolumeClaims(backendPvcNs).Delete(context.TODO(), backendPvcName, deleteOptions)
	klog.Infof(logId + "Deleting (by ownerReference) frontend Pod: \"%s\"", backendPodName )
	klog.Infof(logId + "Deleting (by ownerReference) frontend SVC: \"%s\"", backendSvcName )

	err := p.DclientSet.Resource(veRes).Namespace(backendPvcNs).Delete(context.TODO(), volumeExportName, deleteOptions)
	klog.Infof( logId + "Deleting CRD VolumeExport: \"%s\"", err )
	err = p.DclientSet.Resource(vecRes).Delete(context.TODO(), volumeExportContentName, deleteOptions)
	klog.Infof( logId + "Deleting CRD VolumeExportContent: \"%s\"", err )
	return nil
}

func main() {
	syscall.Umask(0)

	// Cmd Options
	provisionerName := flag.String("name", "nfsexport.rafflescity.io", "Set the provisoner name. Default \"nfsexport.rafflescity.io\"")
	leaderElection := flag.Bool("leader-elect", false, "Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated components for high availability. Default false.")

	flag.Parse()
	flag.Set("logtostderr", "true")

	// Connect to Kubernetes
	kubeconfig := os.Getenv("KUBECONFIG")
	var config *rest.Config
	if kubeconfig != "" {
		// Create an OutOfClusterConfig 
		var err error
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			klog.Fatalf("Failed to create kubeconfig: %v", err)
		}
	} else {
		// Create an InClusterConfig 
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			klog.Fatalf("Failed to create config: %v", err)
		}
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create client: %v", err)
	}
	dclientset, err := dynamic.NewForConfig(config)
	if err != nil {
		klog.Fatalf("Failed to create dynamic client: %v", err)
	}

	// The controller needs to know what the server version is because out-of-tree
	// provisioners aren't officially supported until 1.5
	serverVersion, err := clientset.Discovery().ServerVersion()
	if err != nil {
		klog.Fatalf("Error getting server version: %v", err)
	}

	// Create the provisioner: it implements the Provisioner interface expected by
	// the controller
	volumeNfsProvisioner := NewVolumeNfsProvisioner(clientset, dclientset)

	// Start the provision controller
	// PVs
	pc := controller.NewProvisionController(
		clientset, 
		*provisionerName, 
		volumeNfsProvisioner, 
		serverVersion.GitVersion,
		controller.LeaderElection(*leaderElection),
	)
	
	// Never stops.
	pc.Run(context.Background())
}