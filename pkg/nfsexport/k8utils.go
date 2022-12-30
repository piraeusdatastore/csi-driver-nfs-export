package nfsexport

import (
	//"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/pkg/client/conditions"
	"time"

	"k8s.io/klog/v2"
	"golang.org/x/net/context"
)

// return a condition function that indicates whether the given pod is
// currently running
func isPodRunning(c kubernetes.Interface, podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		klog.V(2).Infof("Waiting for Pod to be ready: %s", podName) // progress bar!

		pod, err := c.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		switch pod.Status.Phase {
		case corev1.PodRunning:
			return true, nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return false, conditions.ErrPodCompleted
		}
		return false, nil
	}
}

// Poll up to timeout seconds for pod to enter running state.
// Returns an error if the pod never enters the running state.
func waitForPodRunning(c kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, isPodRunning(c, podName, namespace))
}
