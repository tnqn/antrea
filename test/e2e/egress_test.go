// Copyright 2021 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"net"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/vmware-tanzu/antrea/pkg/apis/core/v1alpha2"
	"github.com/vmware-tanzu/antrea/pkg/apis/egress/v1alpha1"
)

func TestEgress(t *testing.T) {
	skipIfNotIPv4Cluster(t)
	data, err := setupTest(t)
	if err != nil {
		t.Fatalf("Error when setting up test: %v", err)
	}
	defer teardownTest(t, data)

	//cc := []configChange{
	//	{"Egress", "true", true},
	//}
	//ac := []configChange{
	//	{"Egress", "true", true},
	//}
	//if err := data.mutateAntreaConfigMap(cc, ac, true, true); err != nil {
	//	t.Fatalf("Failed to enable NetworkPolicyStats feature: %v", err)
	//}

	egressNode := controlPlaneNodeName()
	egressNodeIP := controlPlaneNodeIP()
	localIP0 := "1.1.1.10"
	localIP1 := "1.1.1.11"
	serverIP := "1.1.1.20"
	fakeServer := "fakeserver"

	// Create a http server in another netns to fake an external server connected to the egress Node.
	cmd := fmt.Sprintf(`ip netns add %[1]s && \
ip link add dev %[1]s-a type veth peer name %[1]s-b && \
ip link set dev %[1]s-a netns %[1]s && \
ip addr add %[3]s/24 dev %[1]s-b && \
ip addr add %[4]s/24 dev %[1]s-b && \
ip link set dev %[1]s-b up && \
ip netns exec %[1]s ip addr add %[2]s/24 dev %[1]s-a && \
ip netns exec %[1]s ip link set dev %[1]s-a up && \
ip netns exec %[1]s ip route replace default via %[3]s && \
ip netns exec %[1]s /agnhost netexec
`, fakeServer, serverIP, localIP0, localIP1)
	if err := data.createPodOnNode(fakeServer, egressNode, agnhostImage, []string{"sh", "-c", cmd}, nil, nil, nil, true, func(pod *v1.Pod) {
		//pod.Spec.Volumes = []v1.Volume{
		//	{
		//		Name: "host-var-run-netns",
		//		VolumeSource: v1.VolumeSource{
		//			HostPath: &v1.HostPathVolumeSource{Path: "/var/run/netns"},
		//		},
		//	},
		//	{
		//		Name: "host-proc",
		//		VolumeSource: v1.VolumeSource{
		//			HostPath: &v1.HostPathVolumeSource{Path: "/proc"},
		//		},
		//	},
		//}
		//mountPropagation := v1.MountPropagationHostToContainer
		//pod.Spec.Containers[0].VolumeMounts = []v1.VolumeMount{
		//	{
		//		Name: "host-var-run-netns",
		//		MountPath: "/var/run/netns",
		//		ReadOnly: true,
		//		MountPropagation: &mountPropagation,
		//	},
		//	{
		//		Name: "host-proc",
		//		MountPath: "/host/proc",
		//		ReadOnly: true,
		//	},
		//}
		privileged := true
		pod.Spec.Containers[0].SecurityContext = &v1.SecurityContext{Privileged: &privileged}
	}); err != nil {
		t.Fatalf("Failed to create server Pod: %v", err)
	}
	defer deletePodWrapper(t, data, fakeServer)
	if err := data.podWaitForRunning(defaultTimeout, fakeServer, testNamespace); err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", fakeServer)
	}

	localPod := "localpod"
	remotePod := "remotepod"
	if err := data.createBusyboxPodOnNode(localPod, egressNode); err != nil {
		t.Fatalf("Failed to create local Pod: %v", err)
	}
	defer deletePodWrapper(t, data, localPod)
	if err := data.podWaitForRunning(defaultTimeout, localPod, testNamespace); err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", localPod)
	}
	if err := data.createBusyboxPodOnNode(remotePod, workerNodeName(1)); err != nil {
		t.Fatalf("Failed to create remote Pod: %v", err)
	}
	defer deletePodWrapper(t, data, remotePod)
	if err := data.podWaitForRunning(defaultTimeout, remotePod, testNamespace); err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", remotePod)
	}

	//tests := []struct {
	//	name             string
	//	pod              string
	//	egress           v1alpha1.Egress
	//	expectedError    bool
	//	expectedClientIP string
	//}{
	//	{
	//		name: "local pod SNATed to localIP1",
	//		pod:  localPod,
	//		egress: v1alpha1.Egress{
	//			ObjectMeta: metav1.ObjectMeta{Name: "egress-selecting-local-pod"},
	//			Spec: v1alpha1.EgressSpec{
	//				AppliedTo: v1alpha2.AppliedTo{
	//					PodSelector: &metav1.LabelSelector{
	//						MatchLabels: map[string]string{"antrea-e2e": localPod},
	//					},
	//				},
	//				EgressIP: localIP1,
	//			}},
	//		expectedError:    false,
	//		expectedClientIP: localIP1,
	//	},
	//	{
	//		name: "remote pod SNATed to localIP2",
	//		pod:  remotePod,
	//		egress: v1alpha1.Egress{
	//			ObjectMeta: metav1.ObjectMeta{Name: "egress-selecting-remote-pod"},
	//			Spec: v1alpha1.EgressSpec{
	//				AppliedTo: v1alpha2.AppliedTo{
	//					PodSelector: &metav1.LabelSelector{
	//						MatchLabels: map[string]string{"antrea-e2e": remotePod},
	//					},
	//				},
	//				EgressIP: localIP2,
	//			}},
	//		expectedError:    false,
	//		expectedClientIP: localIP2,
	//	},
	//	{
	//		name: "local pod SNATed to default IP",
	//		pod:  localPod,
	//		egress: v1alpha1.Egress{
	//			ObjectMeta: metav1.ObjectMeta{Name: "egress-selecting-none"},
	//			Spec: v1alpha1.EgressSpec{
	//				AppliedTo: v1alpha2.AppliedTo{
	//					PodSelector: &metav1.LabelSelector{
	//						MatchLabels: map[string]string{"antrea-e2e": localPod},
	//					},
	//					// Add a namespace selector matching no namespace.
	//					NamespaceSelector: &metav1.LabelSelector{
	//						MatchLabels: map[string]string{"not-existing-label": ""},
	//					},
	//				},
	//				EgressIP: localIP1,
	//			}},
	//		expectedError:    false,
	//		expectedClientIP: localIP0,
	//	},
	//	{
	//		// Only egress Node can reach the server, so Pods running on other Nodes cannot reach it.
	//		name: "remote pod unable to connect to server",
	//		pod:  remotePod,
	//		egress: v1alpha1.Egress{
	//			ObjectMeta: metav1.ObjectMeta{Name: "egress-selecting-none"},
	//			Spec: v1alpha1.EgressSpec{
	//				AppliedTo: v1alpha2.AppliedTo{
	//					PodSelector: &metav1.LabelSelector{
	//						MatchLabels: map[string]string{"antrea-e2e": remotePod},
	//					},
	//					// Add a namespace selector matching no namespace.
	//					NamespaceSelector: &metav1.LabelSelector{
	//						MatchLabels: map[string]string{"not-existing-label": ""},
	//					},
	//				},
	//				EgressIP: localIP2,
	//			},
	//		},
	//		expectedError: true,
	//	},
	//}
	//for _, tt := range tests {
	//	t.Run(tt.name, func(t *testing.T) {
	//		_, err := data.crdClient.EgressV1alpha1().Egresses().Create(context.TODO(), &tt.egress, metav1.CreateOptions{})
	//		if err != nil {
	//			t.Fatalf("Failed to create Egress %v: %v", tt.egress, err)
	//		}
	//		defer data.crdClient.EgressV1alpha1().Egresses().Delete(context.TODO(), tt.egress.Name, metav1.DeleteOptions{})
	//
	//		time.Sleep(time.Second)
	//		cmd := []string{"curl", "--connect-timeout", "3", serverIP}
	//		stdout, stderr, err := data.runCommandFromPod(testNamespace, tt.pod, getImageName(agnhostImage), cmd)
	//		if tt.expectedError {
	//			assert.Error(t, err)
	//		} else {
	//			assert.NoError(t, err, "unexpected error when executing command %v in Pod %s, stdout: %v, stderr: %v", cmd, tt.pod, stdout, stderr)
	//			assert.Equal(t, tt.expectedClientIP, stdout)
	//		}
	//	})
	//}

	getClientIP := func(pod string) (string, string, error) {
		cmd := []string{"wget", "-T", "3", "-O", "-", fmt.Sprintf("%s:8080/clientip", serverIP)}
		return data.runCommandFromPod(testNamespace, pod, busyboxContainerName, cmd)
	}

	assertClientIP := func(pod string, clientIP string) {
		var exeErr error
		var stdout, stderr string
		if err := wait.Poll(100*time.Millisecond, 2 *time.Second, func() (done bool, err error) {
			stdout, stderr, exeErr = getClientIP(pod)
			if exeErr != nil {
				return false, nil
			}
			// The stdout return is in this format: x.x.x.x:port or [xx:xx:xx::x]:port
			host, _, err := net.SplitHostPort(stdout)
			if err != nil {
				return false, nil
			}
			return host == clientIP, nil
		}); err != nil {
			time.Sleep(time.Hour)
			t.Fatalf("Failed to get expected client IP %s for Pod %s, stdout: %s, stderr: %s, err: %v", clientIP, pod, stdout, stderr, exeErr)
		}
	}

	assertConnError := func(pod string) {
		var exeErr error
		var stdout, stderr string
		if err := wait.Poll(100*time.Millisecond, 2 *time.Second, func() (done bool, err error) {
			stdout, stderr, exeErr = getClientIP(pod)
			if exeErr != nil {
				return true, nil
			}
			return false, nil
		}); err != nil {
			t.Fatalf("Failed to get expected error, stdout: %v, stderr: %v, err: %v", stdout, stderr, exeErr)
		}
	}

	// As the fake server runs in a netns of the Egress Node, only egress Node can reach the server, Pods running on
	// other Nodes cannot reach it before Egress is added.
	assertClientIP(localPod, localIP0)
	assertConnError(remotePod)

	t.Logf("Creating an Egress applying to both Pods")
	egress := &v1alpha1.Egress{
		ObjectMeta: metav1.ObjectMeta{Name: "test-egress"},
		Spec: v1alpha1.EgressSpec{
			AppliedTo: v1alpha2.AppliedTo{
				PodSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "antrea-e2e",
							Operator: metav1.LabelSelectorOpExists,
						},
					},
				},
			},
			// Due to the implementation restriction that SNAT IPs must be reachable from worker Nodes (because SNAT IPs
			// are used as tunnel destination IP), it can only use the Node IP of the Egress Node to verify the case of
			// remote Pod.
			EgressIP: egressNodeIP,
		},
	}
	egress, err = data.crdClient.EgressV1alpha1().Egresses().Create(context.TODO(), egress, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create Egress %v: %v", egress, err)
	}
	defer data.crdClient.EgressV1alpha1().Egresses().Delete(context.TODO(), egress.Name, metav1.DeleteOptions{})
	assertClientIP(localPod, egressNodeIP)
	assertClientIP(remotePod, egressNodeIP)

	t.Log("Updating the Egress's AppliedTo to remotePod only")
	egress.Spec.AppliedTo = v1alpha2.AppliedTo{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"antrea-e2e": remotePod},
		},
	}
	egress, err = data.crdClient.EgressV1alpha1().Egresses().Update(context.TODO(), egress, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update Egress %v: %v", egress, err)
	}
	assertClientIP(localPod, localIP0)
	assertClientIP(remotePod, egressNodeIP)

	t.Log("Updating the Egress's AppliedTo to localPod only")
	egress.Spec.AppliedTo = v1alpha2.AppliedTo{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"antrea-e2e": localPod},
		},
	}
	egress, err = data.crdClient.EgressV1alpha1().Egresses().Update(context.TODO(), egress, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update Egress %v: %v", egress, err)
	}
	assertClientIP(localPod, egressNodeIP)
	assertConnError(remotePod)

	t.Logf("Updating the Egress's EgressIP to %s", localIP1)
	egress.Spec.EgressIP = localIP1
	egress, err = data.crdClient.EgressV1alpha1().Egresses().Update(context.TODO(), egress, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Failed to update Egress %v: %v", egress, err)
	}
	assertClientIP(localPod, localIP1)
	assertConnError(remotePod)

	t.Log("Deleting the Egress")
	err = data.crdClient.EgressV1alpha1().Egresses().Delete(context.TODO(), egress.Name, metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Failed to delete Egress %v: %v", egress, err)
	}
	assertClientIP(localPod, localIP0)
	assertConnError(remotePod)
}
