package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var clientset *kubernetes.Clientset
// var namespace string

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")

	flag.Parse()

	kConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err = kubernetes.NewForConfig(kConfig)
	if err != nil {
		panic(err)
	}

	version, err := getKubernetesVersion(clientset)
	if err != nil {
		panic(err)
	}

	if err := createNetworkPolicy(clientset); err != nil {
        fmt.Println("Error creating network policy:", err)
    } else {
        fmt.Println("Network policy created successfully")
    }

	fmt.Printf("Connected to Kubernetes %s\n", version)

	if err := startServer(*listenAddr); err != nil {
		panic(err)
	}
}

// getKubernetesVersion returns a string GitVersion of the Kubernetes server defined by the clientset.
//
// If it can't connect an error will be returned, which makes it useful to check connectivity.
func getKubernetesVersion(clientset kubernetes.Interface) (string, error) {
	version, err := clientset.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}

	return version.String(), nil
}

// startServer launches an HTTP server with defined handlers and blocks until it's terminated or fails with an error.
//
// Expects a listenAddr to bind to.
func startServer(listenAddr string) error {
	http.HandleFunc("/healthz", healthHandler)

	fmt.Printf("Server listening on %s\n", listenAddr)

	return http.ListenAndServe(listenAddr, nil)
}

func getCurrentNamespace() (string, error) {
    data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
    if err != nil {
        return "", fmt.Errorf("failed to read namespace: %w", err)
    }
    return string(data), nil
}

// healthHandler responds with the health status of the application.
func healthHandler(w http.ResponseWriter, r *http.Request) {

	namespace, err := getCurrentNamespace()
	if err != nil {
		log.Fatalf("Error getting current namespace: %v", err)
	}

	if clientset == nil {
        http.Error(w, "Kubernetes client not initialized", http.StatusInternalServerError)
        return
    }

	deploymentsClient := clientset.AppsV1().Deployments(namespace)
	list, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		http.Error(w, "Failed to list deployments", http.StatusInternalServerError)
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
    err = json.NewEncoder(w).Encode(healthStatus)

	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		fmt.Println("Failed writing to response", err)
		return
	}
}

// create a network policy that denies all traffic in and out of the default namespace
func createNetworkPolicy(clientset *kubernetes.Clientset) error {
	namespace, err := getCurrentNamespace()
	if err != nil {
		log.Fatalf("Error getting current namespace: %v", err)
	}

    policy := &networkingv1.NetworkPolicy{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "deny-cross-namespace-traffic",
            Namespace: string(namespace),
        },
        Spec: networkingv1.NetworkPolicySpec{
            PodSelector: metav1.LabelSelector{}, // Selects all pods in the namespace
            PolicyTypes: []networkingv1.PolicyType{
                networkingv1.PolicyTypeIngress,
                networkingv1.PolicyTypeEgress,
            },
            Ingress: []networkingv1.NetworkPolicyIngressRule{}, // Deny all ingress
            Egress: []networkingv1.NetworkPolicyEgressRule{},   // Deny all egress
        },
    }

	_, err = clientset.NetworkingV1().NetworkPolicies(string(namespace)).Create(context.TODO(), policy, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create network policy: %v", err)
	}
	return nil
}