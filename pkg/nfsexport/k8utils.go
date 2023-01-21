package nfsexport

import (
	"fmt"
	"net"
	"time"
	"strconv"
	"os"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/client/conditions"
	"k8s.io/klog/v2"
	"golang.org/x/net/context"

	probe "k8s.io/kubernetes/pkg/probe"
	probetcp"k8s.io/kubernetes/pkg/probe/tcp"

)

// return a condition function that indicates whether the given pod is
// currently running
// func isPodRunning(c kubernetes.Interface, podName, namespace string) wait.ConditionFunc {
// 	return func() (bool, error) {
// 		klog.V(2).Infof("Waiting for Pod to be ready: %s", podName) // progress bar!

// 		pod, err := c.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
// 		if err != nil {
// 			return false, err
// 		}

// 		switch pod.Status.Phase {
// 		case corev1.PodRunning:
// 			return true, nil
// 		case corev1.PodFailed, corev1.PodSucceeded:
// 			return false, conditions.ErrPodCompleted
// 		}
// 		return false, nil
// 	}
// }

// // Poll up to timeout seconds for pod to enter running state.
// // Returns an error if the pod never enters the running state.
// func waitForPodRunning(c kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
// 	return wait.PollImmediate(time.Second, timeout, isPodRunning(c, podName, namespace))
// }

func isPodRunning(c kubernetes.Interface, podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := c.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		klog.V(2).Infof("POD %s state: %s", podName, pod.Status.Phase)
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return true, nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return false, conditions.ErrPodCompleted
		}
		return false, nil
	}
}

func isPodNotRunning(c kubernetes.Interface, podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := c.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return true, err
		}
		klog.V(2).Infof("POD %s state: %s", podName, pod.Status.Phase)
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return false, nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return true, conditions.ErrPodCompleted
		}
		return true, nil
	}
}

// Poll up to timeout seconds for pod to enter running state.
// Returns an error if the pod never enters the running state.
func waitForPodRunning(c kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, isPodRunning(c, podName, namespace))
}

func waitForPodNotRunning(c kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, isPodNotRunning(c, podName, namespace))
}


func isTcpReady(host string, port int) wait.ConditionFunc {
	return func() (bool, error) {
		timeout := time.Second
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		result, errStr, err := probetcp.DoTCPProbe(addr, timeout)
		klog.V(2).Infof("%s", errStr)
        if err != nil {
			klog.V(2).Infof("%s", err)
            return false, err
        }
        if result == probe.Success {
            klog.V(2).Infof("Dial TCP %s succeeds", addr)
            return true, nil
        }
		return false, nil
	}
}

func waitForTcpReady(host string, port int, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, isTcpReady(host, port))
}

func getPVCByUidStr(c kubernetes.Interface, uid string ) (*corev1.PersistentVolumeClaim, error) {
	pvcList, err := c.CoreV1().PersistentVolumeClaims("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Unable to get PVC List")
	}
	var pvc corev1.PersistentVolumeClaim
	for _, pvc = range pvcList.Items {
		if string(pvc.ObjectMeta.UID) == uid {
			break
		}
	}
	if pvc.ObjectMeta.Name == "" {
		return nil, fmt.Errorf("Unable find the PVC with UID: %s", uid )
	} else {
		return &pvc, nil
	}
}

func getPvById(c kubernetes.Interface, id string) (*corev1.PersistentVolume, error) {
	pvList, err := c.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("Unable to get PV List")
	}
	var pv corev1.PersistentVolume
	for _, pv = range pvList.Items {
		if pv.Spec.PersistentVolumeSource.CSI != nil && 
		   pv.Spec.PersistentVolumeSource.CSI.VolumeHandle == id {
		   return &pv, nil
		}
	}
	return nil, fmt.Errorf("Unable find the PVC with CSI ID: %s", id )
}

func Ptr[T any](v T) *T {
    return &v
}

func dirExists(dirname string) bool {
	info, err := os.Stat(dirname)
	if err == nil && info.IsDir() {
	   return true
	} else {
		klog.V(2).Infof("%s", err)
	}
	return false
 }
