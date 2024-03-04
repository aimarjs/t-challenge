package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"path/filepath"

	"github.com/gorilla/mux"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	router := mux.NewRouter()
	router.HandleFunc("/health", getClusterHealth).Methods("GET")
	http.ListenAndServe(":8080", router)
}

func getClusterHealth(w http.ResponseWriter, r *http.Request) {
	config, err := clientcmd.BuildConfigFromFlags("", filepath.Join(os.Getenv("HOME"), ".kube", "config"))
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	deploymentsClient := clientset.AppsV1().Deployments(metav1.NamespaceDefault)
	list, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

    healthStatus := make(map[string]string)
    for _, d := range list.Items {
        if d.Status.ReadyReplicas == *d.Spec.Replicas {
            healthStatus[d.Name] = "Healthy"
        } else {
            healthStatus[d.Name] = "Unhealthy"
        }
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(healthStatus)
}