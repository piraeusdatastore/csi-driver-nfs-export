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

package main

import (
	"flag"
	"os"
	"k8s.io/klog/v2"
	"github.com/rafflescity/csi-driver-nfs-export/pkg/nfsexport"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/rest"
)

var (
	endpoint         = flag.String("endpoint", "unix://tmp/csi.sock", "CSI endpoint")
	nodeID           = flag.String("nodeid", "", "node id")
	mountPermissions = flag.Uint64("mount-permissions", 0, "mounted folder permissions")
	driverName       = flag.String("drivername", nfsexport.DefaultDriverName, "name of the driver")
	workingMountDir  = flag.String("working-mount-dir", "/tmp", "working directory for provisioner to mount nfs shares temporarily")
)

func init() {
	_ = flag.Set("logtostderr", "true")
}

func main() {
	klog.InitFlags(nil)
	flag.Parse()
	if *nodeID == "" {
		klog.Warning("nodeid is empty")
	}

	handle()
	os.Exit(0)
}

func handle() {
	driverOptions := nfsexport.DriverOptions{
		NodeID:           *nodeID,
		DriverName:       *driverName,
		Endpoint:         *endpoint,
		MountPermissions: *mountPermissions,
		WorkingMountDir:  *workingMountDir,
	}
	d := nfsexport.NewDriver(&driverOptions, getKubeClient())
	d.Run(false)
}

func getKubeClient()(*kubernetes.Clientset) {
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
	return clientset
}
