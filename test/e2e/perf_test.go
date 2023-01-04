package e2e

import (
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	iperf3Image  = "networkstatic/iperf3:latest"
	netperfImage = "networkstatic/netperf:latest"

	totalRounds = 5
)

type iperf3ResultSum struct {
	BitsPerSecond float64 `json:"bits_per_second"`
	Retransmits   int32   `json:"retransmits"`
}

type iperf3ResultEnd struct {
	SumSent iperf3ResultSum `json:"sum_sent"`
}

type iperf3Result struct {
	End iperf3ResultEnd `json:"end"`
}

func TestPerfThroughput(t *testing.T) {
	data, err := setupTest(t)
	if err != nil {
		t.Fatalf("Error when setting up test: %v", err)
	}
	defer teardownTest(t, data)

	node0Name := nodeName(1)
	if testOptions.node0 != "" {
		node0Name = testOptions.node0
	}
	node1Name := nodeName(2)
	if testOptions.node1 != "" {
		node1Name = testOptions.node1
	}
	clientPodName := "client"
	server0PodName := "server0"
	server1PodName := "server1"
	server0SvcName := "server0"
	server1SvcName := "server1"
	if err := NewPodBuilder(clientPodName, data.testNamespace, iperf3Image).OnNode(node0Name).WithCommand([]string{"sleep", "infinity"}).Create(data); err != nil {
		t.Fatalf("Failed to create client Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, clientPodName)
	if err := NewPodBuilder(server0PodName, data.testNamespace, iperf3Image).OnNode(node0Name).WithCommand([]string{"iperf3", "-s"}).Create(data); err != nil {
		t.Fatalf("Failed to create server0 Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, server0PodName)
	if err := NewPodBuilder(server1PodName, data.testNamespace, iperf3Image).OnNode(node1Name).WithCommand([]string{"iperf3", "-s"}).Create(data); err != nil {
		t.Fatalf("Failed to create server1 Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, server1PodName)

	if err := data.podWaitForRunning(defaultTimeout, clientPodName, data.testNamespace); err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}
	server0PodIPs, err := data.podWaitForIPs(defaultTimeout, server0PodName, data.testNamespace)
	if err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}
	server1PodIPs, err := data.podWaitForIPs(defaultTimeout, server1PodName, data.testNamespace)
	if err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}

	server0Svc, err := data.CreateService(server0SvcName, data.testNamespace, 5201, 5201, map[string]string{"antrea-e2e": server0PodName}, false, false, corev1.ServiceTypeClusterIP, nil)
	if err != nil {
		t.Fatalf("Failed to create server1 Service: %v", err)
	}
	server1Svc, err := data.CreateService(server1SvcName, data.testNamespace, 5201, 5201, map[string]string{"antrea-e2e": server1PodName}, false, false, corev1.ServiceTypeClusterIP, nil)
	if err != nil {
		t.Fatalf("Failed to create server1 Service: %v", err)
	}

	testCmd := []string{"iperf3", "-c", server0PodIPs.ipv4.String(), "-J", "-t", "10"}
	result, err := runPerfTest(t, data, clientPodName, testCmd, getBitsPerSecond)
	if err != nil {
		t.Fatalf("Failed to run throughput test: %v", err)
	}
	t.Logf("Intra-Node Pods throughput: %.2f Gbps", result/(1000*1000*1000))

	testCmd = []string{"iperf3", "-c", server1PodIPs.ipv4.String(), "-J", "-t", "10"}
	result, err = runPerfTest(t, data, clientPodName, testCmd, getBitsPerSecond)
	if err != nil {
		t.Fatalf("Failed to run throughput test: %v", err)
	}
	t.Logf("Inter-Node Pods throughput: %.2f Gbps", result/(1000*1000*1000))

	testCmd = []string{"iperf3", "-c", server0Svc.Spec.ClusterIP, "-J", "-t", "10"}
	result, err = runPerfTest(t, data, clientPodName, testCmd, getBitsPerSecond)
	if err != nil {
		t.Fatalf("Failed to run throughput test: %v", err)
	}
	t.Logf("Intra-Node Pod-to-Service throughput: %.2f Gbps", result/(1000*1000*1000))

	testCmd = []string{"iperf3", "-c", server1Svc.Spec.ClusterIP, "-J", "-t", "10"}
	result, err = runPerfTest(t, data, clientPodName, testCmd, getBitsPerSecond)
	if err != nil {
		t.Fatalf("Failed to run throughput test: %v", err)
	}
	t.Logf("Inter-Node Pod-to-Service throughput: %.2f Gbps", result/(1000*1000*1000))
}

func getBitsPerSecond(s string) (float64, error) {
	var result iperf3Result
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return 0, err
	}
	return result.End.SumSent.BitsPerSecond, nil
}

func TestPerfTCPRR(t *testing.T) {
	data, err := setupTest(t)
	if err != nil {
		t.Fatalf("Error when setting up test: %v", err)
	}
	defer teardownTest(t, data)

	node0Name := nodeName(1)
	if testOptions.node0 != "" {
		node0Name = testOptions.node0
	}
	node1Name := nodeName(2)
	if testOptions.node1 != "" {
		node1Name = testOptions.node1
	}
	clientPodName := "client"
	server0PodName := "server0"
	server1PodName := "server1"
	if err := NewPodBuilder(clientPodName, data.testNamespace, netperfImage).OnNode(node0Name).WithCommand([]string{"sleep", "infinity"}).Create(data); err != nil {
		t.Fatalf("Failed to create client Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, clientPodName)
	if err := NewPodBuilder(server0PodName, data.testNamespace, netperfImage).OnNode(node0Name).WithCommand([]string{"netserver", "-D"}).Create(data); err != nil {
		t.Fatalf("Failed to create server0 Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, server0PodName)
	if err := NewPodBuilder(server1PodName, data.testNamespace, netperfImage).OnNode(node1Name).WithCommand([]string{"netserver", "-D"}).Create(data); err != nil {
		t.Fatalf("Failed to create server1 Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, server1PodName)

	if err := data.podWaitForRunning(defaultTimeout, clientPodName, data.testNamespace); err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}
	server0PodIPs, err := data.podWaitForIPs(defaultTimeout, server0PodName, data.testNamespace)
	if err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}
	server1PodIPs, err := data.podWaitForIPs(defaultTimeout, server1PodName, data.testNamespace)
	if err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}

	testCmd := []string{"netperf", "-H", server0PodIPs.ipv4.String(), "-t", "TCP_RR", "-P", "0", "-l", "10"}
	result, err := runPerfTest(t, data, clientPodName, testCmd, getTransPerSecond)
	if err != nil {
		t.Fatalf("Failed to run TCP_RR test: %v", err)
	}
	t.Logf("Intra-Node Pods TCP_RR: %.2f Trans/s", result)

	testCmd = []string{"netperf", "-H", server1PodIPs.ipv4.String(), "-t", "TCP_RR", "-P", "0", "-l", "10"}
	result, err = runPerfTest(t, data, clientPodName, testCmd, getTransPerSecond)
	if err != nil {
		t.Fatalf("Failed to run TCP_RR test: %v", err)
	}
	t.Logf("Inter-Node Pods TCP_RR: %.2f Trans/s", result)
}

func TestPerfTCPCRR(t *testing.T) {
	data, err := setupTest(t)
	if err != nil {
		t.Fatalf("Error when setting up test: %v", err)
	}
	defer teardownTest(t, data)

	node0Name := nodeName(1)
	if testOptions.node0 != "" {
		node0Name = testOptions.node0
	}
	node1Name := nodeName(2)
	if testOptions.node1 != "" {
		node1Name = testOptions.node1
	}
	clientPodName := "client"
	server0PodName := "server0"
	server1PodName := "server1"
	if err := NewPodBuilder(clientPodName, data.testNamespace, netperfImage).OnNode(node0Name).WithCommand([]string{"sleep", "infinity"}).Create(data); err != nil {
		t.Fatalf("Failed to create client Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, clientPodName)
	if err := NewPodBuilder(server0PodName, data.testNamespace, netperfImage).OnNode(node0Name).WithCommand([]string{"netserver", "-D"}).Create(data); err != nil {
		t.Fatalf("Failed to create server0 Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, server0PodName)
	if err := NewPodBuilder(server1PodName, data.testNamespace, netperfImage).OnNode(node1Name).WithCommand([]string{"netserver", "-D"}).Create(data); err != nil {
		t.Fatalf("Failed to create server1 Pod: %v", err)
	}
	defer deletePodWrapper(t, data, data.testNamespace, server1PodName)

	if err := data.podWaitForRunning(defaultTimeout, clientPodName, data.testNamespace); err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}
	server0PodIPs, err := data.podWaitForIPs(defaultTimeout, server0PodName, data.testNamespace)
	if err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}
	server1PodIPs, err := data.podWaitForIPs(defaultTimeout, server1PodName, data.testNamespace)
	if err != nil {
		t.Fatalf("Error when waiting for Pod '%s' to be in the Running state", clientPodName)
	}

	testCmd := []string{"netperf", "-H", server0PodIPs.ipv4.String(), "-t", "TCP_CRR", "-P", "0", "-l", "10"}
	result, err := runPerfTest(t, data, clientPodName, testCmd, getTransPerSecond)
	if err != nil {
		t.Fatalf("Failed to run TCP_CRR test: %v", err)
	}
	t.Logf("Intra-Node Pods TCP_CRR: %.2f Trans/s", result)

	time.Sleep(1 * time.Minute)
	testCmd = []string{"netperf", "-H", server1PodIPs.ipv4.String(), "-t", "TCP_CRR", "-P", "0", "-l", "10"}
	result, err = runPerfTest(t, data, clientPodName, testCmd, getTransPerSecond)
	if err != nil {
		t.Fatalf("Failed to run TCP_CRR test: %v", err)
	}
	t.Logf("Inter-Node Pods TCP_CRR: %.2f Trans/s", result)
}

func runPerfTest(t *testing.T, data *TestData, clientPodName string, cmd []string, resultParser func(string) (float64, error)) (float64, error) {
	var results []float64
	for i := 0; i < totalRounds; i++ {
		stdout, _, err := data.RunCommandFromPod(data.testNamespace, clientPodName, "", cmd)
		if err != nil {
			return 0, fmt.Errorf("error when running perf test '%v' in Pod %s: %v", cmd, clientPodName, err)
		}
		//t.Logf("Stdout: %s", stdout)
		result, err := resultParser(stdout)
		if err != nil {
			return 0, fmt.Errorf("error when parsing perf test result '%s': %v", stdout, err)
		}
		//t.Logf("Result :%v", result)
		results = append(results, result)
	}
	t.Logf("Results :%v", results)
	sort.Float64s(results)
	results = results[1 : len(results)-1]
	var sum float64
	for _, result := range results {
		sum += result
	}
	average := sum / float64(len(results))
	return average, nil
}

func getTransPerSecond(s string) (float64, error) {
	lines := strings.Split(s, "\n")
	line := strings.TrimRight(lines[0], " ")
	lastItem := line[strings.LastIndex(line, " ")+1:]
	ret, _ := strconv.ParseFloat(lastItem, 64)
	return ret, nil
}
