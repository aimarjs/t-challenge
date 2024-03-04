package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetClusterHealth(t *testing.T) {
	// Set up router and request
	router := mux.NewRouter()
	router.HandleFunc("/health", getClusterHealth)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Set up fake client
	client := fake.NewSimpleClientset()
	deploy := metav1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "deploy1",
		},
		Status: metav1.DeploymentStatus{
			ReadyReplicas: 2,
			Replicas:      2,
		},
	}
	client.AppsV1().Deployments("").Create(&deploy)

	// Make request
	router.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status OK but got %v", rec.Code)
	}

	var healthStatus map[string]string
	json.Unmarshal(rec.Body.Bytes(), &healthStatus)

	expected := map[string]string{"deploy1": "Healthy"}
	if healthStatus["deploy1"] != expected["deploy1"] {
		t.Errorf("Expected %v but got %v", expected, healthStatus)
	}
}

func TestGetClusterHealth_ErrorGettingDeployments(t *testing.T) {
	// Set up router and request
	router := mux.NewRouter()
	router.HandleFunc("/health", getClusterHealth)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Return error from deployments client
	client := &fake.Clientset{}
	client.AddReactor("list", "deployments", func(action fake.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("error getting deployments")
	})

	// Make request
	router.ServeHTTP(rec, req)

	// Check response
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status Internal Server Error but got %v", rec.Code)
	}
}

