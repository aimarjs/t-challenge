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

	http.HandleFunc("/enable-isolation", enableIsolationHandler)
    http.HandleFunc("/disable-isolation", disableIsolationHandler)

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

func enableIsolationHandler(w http.ResponseWriter, r *http.Request) {
    err := applyIsolationToAllNamespaces()
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to apply isolation policies: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    fmt.Fprintln(w, "Isolation enabled across all namespaces")
}

func disableIsolationHandler(w http.ResponseWriter, r *http.Request) {
    policyName := "isolation-policy"

    namespace, err := getCurrentNamespace()
    if err != nil {
        http.Error(w, fmt.Sprintf("Error getting current namespace: %v", err), http.StatusInternalServerError)
        return
    }

    err = clientset.NetworkingV1().NetworkPolicies(namespace).Delete(context.TODO(), policyName, metav1.DeleteOptions{})
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to remove isolation policy: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    fmt.Fprintln(w, "Isolation disabled")
}

func applyIsolationToAllNamespaces() error {
    namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
    if err != nil {
        return fmt.Errorf("failed to list namespaces: %v", err)
    }

    for _, ns := range namespaces.Items {
        policy := createIsolationNetworkPolicy(ns.Name)
        _, err := clientset.NetworkingV1().NetworkPolicies(ns.Name).Create(context.TODO(), policy, metav1.CreateOptions{})
        if err != nil {
            // Consider logging the error instead of immediately returning
            log.Printf("Failed to apply isolation policy to namespace %s: %v", ns.Name, err)
            // Continue attempting to apply to other namespaces
        }
    }

    return nil // or detailed error aggregation if needed
}

func createIsolationNetworkPolicy(namespace string) *networkingv1.NetworkPolicy {
    // This is a very basic example. Customize it based on your actual requirements.
    return &networkingv1.NetworkPolicy{
        ObjectMeta: metav1.ObjectMeta{
            Name: "isolation-policy",
			Namespace: namespace,
        },
        Spec: networkingv1.NetworkPolicySpec{
            PodSelector: metav1.LabelSelector{}, // Selects all pods
            PolicyTypes: []networkingv1.PolicyType{
                networkingv1.PolicyTypeIngress,
                networkingv1.PolicyTypeEgress,
            },
            Ingress: []networkingv1.NetworkPolicyIngressRule{}, // Deny all ingress
            // Egress:  []networkingv1.NetworkPolicyEgressRule{},  // Deny all egress
        },
    }
}