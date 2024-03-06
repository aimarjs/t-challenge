package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	disco "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetKubernetesVersion(t *testing.T) {
	okClientset := fake.NewSimpleClientset()
	okClientset.Discovery().(*disco.FakeDiscovery).FakedServerVersion = &version.Info{GitVersion: "1.25.0-fake"}

	okVer, err := getKubernetesVersion(okClientset)
	assert.NoError(t, err)
	assert.Equal(t, "1.25.0-fake", okVer)

	badClientset := fake.NewSimpleClientset()
	badClientset.Discovery().(*disco.FakeDiscovery).FakedServerVersion = &version.Info{}

	badVer, err := getKubernetesVersion(badClientset)
	assert.NoError(t, err)
	assert.Equal(t, "", badVer)
}

func TestHealthHandlerOk(t *testing.T) {
	mockNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}

	mockDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
		},
	}

	clientset := fake.NewSimpleClientset(mockDeployment, mockNamespace)

	server := &Server{
		Clientset: clientset,
	}

	req, err := http.NewRequest("GET", "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(server.healthHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]map[string]string
	err = json.NewDecoder(rr.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if healthStatus, found := response["test-namespace"]["test-pod"]; found {
		if healthStatus != "Healthy" {
			t.Errorf("Expected 'Healthy' status for 'test-pod', got '%s'", healthStatus)
		}
	} else {
		t.Error("Deployment 'test-pod' not found in response")
	}
}

func TestHealthHandlerUnhealthy(t *testing.T) {

	mockNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}

	mockDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
		},
	}

	clientset := fake.NewSimpleClientset(mockNamespace, mockDeployment)
	server := &Server{
		Clientset: clientset,
	}

	req, err := http.NewRequest("GET", "/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(server.healthHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response map[string]map[string]string
	err = json.NewDecoder(rr.Body).Decode(&response)
	if err != nil {
		t.Fatalf("Failed to decode response body: %v", err)
	}

	if healthStatus, found := response["test-namespace"]["test-pod"]; found {
		if healthStatus != "Unhealthy" {
			t.Errorf("Expected 'Unhealthy' status for 'test-pod', got '%s'", healthStatus)
		}
	} else {
		t.Error("Deployment 'test-pod' not found in response")
	}
}

func TestStatusHandler(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	server := &Server{
		Clientset: clientset,
	}

	req, err := http.NewRequest("GET", "/status", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.statusHandler)

	handler.ServeHTTP(rr, req)

    if status := rr.Code; status != http.StatusOK {
        t.Errorf("statusHandler returned wrong status code: got %v want %v", status, http.StatusOK)
    }

    if !strings.Contains(rr.Body.String(), "Kubernetes API communication successful. Version:") {
        t.Errorf("Expected success message, got %s", rr.Body.String())
    }
}

func TestEnableIsolationHandler(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	server := &Server{Clientset: clientset}

	isolationRequest := struct {
		Workload1 Workload `json:"workload1"`
		Workload2 Workload `json:"workload2"`
	}{
		Workload1: Workload{Namespace: "namespace1", Labels: map[string]string{"app": "app1"}},
		Workload2: Workload{Namespace: "namespace2", Labels: map[string]string{"app": "app2"}},
	}
	body, err := json.Marshal(isolationRequest)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}
	req := httptest.NewRequest("POST", "/enable-isolation", bytes.NewBuffer(body))
	rr := httptest.NewRecorder()

	handler := http.HandlerFunc(server.enableIsolationHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	expected := "Isolation enabled between specified workloads\n"
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}

func TestApplyIsolationBetweenWorkloadsSuccess(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	workload1 := Workload{
		Namespace: "namespace1",
		Labels:    map[string]string{"app": "app1"},
	}
	workload2 := Workload{
		Namespace: "namespace2",
		Labels:    map[string]string{"app": "app2"},
	}

	err := applyIsolationBetweenWorkloads(clientset, workload1, workload2)
	if err != nil {
		t.Fatalf("applyIsolationBetweenWorkloads() failed: %v", err)
	}

	policies, err := clientset.NetworkingV1().NetworkPolicies(workload1.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil || len(policies.Items) != 1 {
		t.Fatalf("Expected 1 network policy in %s, found %d", workload1.Namespace, len(policies.Items))
	}

	policies, err = clientset.NetworkingV1().NetworkPolicies(workload2.Namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil || len(policies.Items) != 1 {
		t.Fatalf("Expected 1 network policy in %s, found %d", workload2.Namespace, len(policies.Items))
	}
}

func TestGenerateNetworkPolicy (t *testing.T) {
	name := "test-policy"
	namespace := "test-namespace"
	podLabels := map[string]string{"app": "myApp"}
	blockLabels := map[string]string{"app": "blockApp"}

	expectedPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: podLabels,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: blockLabels,
					},
				}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					PodSelector: &metav1.LabelSelector{
						MatchLabels: blockLabels,
					},
				}},
			}},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
		},
	}

	actualPolicy := generateNetworkPolicy(name, namespace, podLabels, blockLabels)

	if !reflect.DeepEqual(actualPolicy, expectedPolicy) {
		t.Errorf("generateNetworkPolicy() = %v, want %v", actualPolicy, expectedPolicy)
	}
}

func newRequestWithBody(method, url string, body interface{}) (*http.Request, error) {
    requestBody, err := json.Marshal(body)
    if err != nil {
        return nil, err
    }
    return http.NewRequest(method, url, bytes.NewBuffer(requestBody))
}

func TestDisableIsolationHandlerValidRequest(t *testing.T) {
	ns1ToNs2Policy := &networkingv1.NetworkPolicy{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "isolate-ns1-from-ns2",
            Namespace: "ns1",
        },
        Spec: networkingv1.NetworkPolicySpec{},
    }

    ns2ToNs1Policy := &networkingv1.NetworkPolicy{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "isolate-ns2-from-ns1",
            Namespace: "ns2",
        },
        Spec: networkingv1.NetworkPolicySpec{},
    }
    clientset := fake.NewSimpleClientset(ns1ToNs2Policy, ns2ToNs1Policy)
    server := &Server{Clientset: clientset}

    isolationRequest := struct {
        Workload1 Workload `json:"workload1"`
        Workload2 Workload `json:"workload2"`
    }{
        Workload1: Workload{Namespace: "ns1", Labels: map[string]string{"app": "app1"}},
        Workload2: Workload{Namespace: "ns2", Labels: map[string]string{"app": "app2"}},
    }

    req, err := newRequestWithBody("POST", "/disable-isolation", isolationRequest)
    if err != nil {
        t.Fatal(err)
    }

    rr := httptest.NewRecorder()
    handler := http.HandlerFunc(server.disableIsolationHandler)
    handler.ServeHTTP(rr, req)

    if status := rr.Code; status != http.StatusOK {
        t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
    }

    expected := "Isolation disabled between specified workloads\n"
    if rr.Body.String() != expected {
        t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
    }
}

func TestDisableIsolationHandlerInvalidRequest(t *testing.T) {
    server := &Server{Clientset: fake.NewSimpleClientset()}

    req, err := newRequestWithBody("POST", "/disable-isolation", "{invalidJSON}")
    if err != nil {
        t.Fatal(err)
    }

    rr := httptest.NewRecorder()
    handler := http.HandlerFunc(server.disableIsolationHandler)
    handler.ServeHTTP(rr, req)

    if status := rr.Code; status != http.StatusBadRequest {
        t.Errorf("handler returned wrong status code for invalid request: got %v want %v", status, http.StatusBadRequest)
    }
}

func TestDeleteIsolationPoliciesSuccess(t *testing.T) {
	clientset := fake.NewSimpleClientset(
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "isolate-ns1-from-ns2",
				Namespace: "ns1",
			},
		},
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "isolate-ns2-from-ns1",
				Namespace: "ns2",
			},
		},
	)

	workload1 := Workload{Namespace: "ns1", Labels: map[string]string{"app": "app1"}}
	workload2 := Workload{Namespace: "ns2", Labels: map[string]string{"app": "app2"}}

	err := deleteIsolationPolicies(clientset, workload1, workload2)
	if err != nil {
		t.Errorf("deleteIsolationPolicies() returned an error: %v", err)
	}

	policies, err := clientset.NetworkingV1().NetworkPolicies("ns1").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "metadata.name=isolate-ns1-from-ns2",
	})
	if err != nil || len(policies.Items) != 0 {
		t.Errorf("Expected network policy %s in namespace %s to be deleted", "isolate-ns1-from-ns2", "ns1")
	}

	policies, err = clientset.NetworkingV1().NetworkPolicies("ns2").List(context.TODO(), metav1.ListOptions{
		FieldSelector: "metadata.name=isolate-ns2-from-ns1",
	})
	if err != nil || len(policies.Items) != 0 {
		t.Errorf("Expected network policy %s in namespace %s to be deleted", "isolate-ns2-from-ns1", "ns2")
	}
}

// Simulating a failure scenario with fake.Clientset directly is more challenging since it's designed to simplify testing by avoiding failure.
// Failure might be better suit integration or E2E tests where having more control over the cluster state.
// func TestDeleteIsolationPoliciesError(t *testing.T) {}