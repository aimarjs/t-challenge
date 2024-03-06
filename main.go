package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type Workload struct {
    Namespace string            `json:"namespace"`
    Labels    map[string]string `json:"labels"`
}

type Server struct {
	Clientset kubernetes.Interface
}

func main() {
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig, leave empty for in-cluster")
	listenAddr := flag.String("address", ":8080", "HTTP server listen address")

	flag.Parse()

	kConfig, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(kConfig)
	if err != nil {
		panic(err)
	}

	version, err := getKubernetesVersion(clientset)
	if err != nil {
		panic(err)
	}

	server := &Server{
		Clientset: clientset,
	}

	fmt.Printf("Connected to Kubernetes %s\n", version)

	if err := server.StartServer(*listenAddr); err != nil {
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
func (s *Server) StartServer(listenAddr string) error {
	http.HandleFunc("/healthz", s.healthHandler)
	http.HandleFunc("/status", s.statusHandler)
	http.HandleFunc("/enable-isolation", s.enableIsolationHandler)
	http.HandleFunc("/disable-isolation", s.disableIsolationHandler)

	fmt.Printf("Server listening on %s\n", listenAddr)

	return http.ListenAndServe(listenAddr, nil)
}

// healthHandler responds with the health status of the application.
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	namespaces, err := s.Clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list the namespaces: %v", err), http.StatusInternalServerError)
		return
	}

	allNamespacesHealthStatus := make(map[string]map[string]string)

	for _, namespace := range namespaces.Items {
		deploymentsClient := s.Clientset.AppsV1().Deployments(namespace.Name)
		list, err := deploymentsClient.List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list deployments in namespace %s: %v", namespace.Name, err), http.StatusInternalServerError)
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

		allNamespacesHealthStatus[namespace.Name] = healthStatus
	}

	w.Header().Set("Content-Type", "application/json")
	err = json.NewEncoder(w).Encode(allNamespacesHealthStatus)

	if err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		fmt.Println("Failed writing to response", err)
		return
	}
}

// statusHandler responds with the health status of the kube api.
func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	version, err := getKubernetesVersion(s.Clientset)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to communicate with Kubernetes API: %v", err), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Kubernetes API communication successful. Version: %s\n", version)
}

// will enable isolation between two specified workloads
func (s *Server) enableIsolationHandler(w http.ResponseWriter, r *http.Request) {
    var isolationRequest struct {
        Workload1 Workload `json:"workload1"`
        Workload2 Workload `json:"workload2"`
    }

    if err := json.NewDecoder(r.Body).Decode(&isolationRequest); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    err := applyIsolationBetweenWorkloads(s.Clientset, isolationRequest.Workload1, isolationRequest.Workload2)
    if err != nil {
        http.Error(w, fmt.Sprintf("Failed to apply isolation policies: %v", err), http.StatusInternalServerError)
        return
    }

    w.WriteHeader(http.StatusOK)
    fmt.Fprintln(w, "Isolation enabled between specified workloads")
}

// IsolationBetweenWorkloads creates network policies to isolate two workloads from each other.
func applyIsolationBetweenWorkloads(clientset kubernetes.Interface ,workload1, workload2 Workload) error {
	policy1 := generateNetworkPolicy("isolate-"+workload1.Namespace+"-from-"+workload2.Namespace, workload1.Namespace, workload1.Labels, workload2.Labels)

	_, err := clientset.NetworkingV1().NetworkPolicies(workload1.Namespace).Create(context.TODO(), policy1, metav1.CreateOptions{})
    if err != nil {
        return fmt.Errorf("failed to apply isolation policy to namespace %s: %w", workload1.Namespace, err)
    }

	policy2 := generateNetworkPolicy("isolate-"+workload2.Namespace+"-from-"+workload1.Namespace, workload2.Namespace, workload2.Labels, workload1.Labels)

	_, err = clientset.NetworkingV1().NetworkPolicies(workload2.Namespace).Create(context.TODO(), policy2, metav1.CreateOptions{})
    if err != nil {
        return fmt.Errorf("failed to apply isolation policy to namespace %s: %w", workload2.Namespace, err)
    }

    return nil
}

// generateNetworkPolicy creates a NetworkPolicy object to block traffic between pods with specified labels.
func generateNetworkPolicy(name, namespace string, podLabels, blockLabels map[string]string) *networkingv1.NetworkPolicy {
    return &networkingv1.NetworkPolicy{
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
}

// will disable isolation between two specified workloads
func (s *Server) disableIsolationHandler(w http.ResponseWriter, r *http.Request) {
    var isolationRequest struct {
        Workload1 Workload `json:"workload1"`
        Workload2 Workload `json:"workload2"`
    }

    if err := json.NewDecoder(r.Body).Decode(&isolationRequest); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

	if err := deleteIsolationPolicies(s.Clientset, isolationRequest.Workload1, isolationRequest.Workload2); err != nil {
		http.Error(w, fmt.Sprintf("Failed to disable isolation policies: %v", err), http.StatusInternalServerError)
		return
	}

    w.WriteHeader(http.StatusOK)
    fmt.Fprintln(w, "Isolation disabled between specified workloads")
}

// deleteIsolationPolicies removes network policies that isolate two workloads from each other.
func deleteIsolationPolicies(clientset kubernetes.Interface, workload1, workload2 Workload) error {
    policyName1 := fmt.Sprintf("isolate-%s-from-%s", workload1.Namespace, workload2.Namespace)
    policyName2 := fmt.Sprintf("isolate-%s-from-%s", workload2.Namespace, workload1.Namespace)

    if err := clientset.NetworkingV1().NetworkPolicies(workload1.Namespace).Delete(context.TODO(), policyName1, metav1.DeleteOptions{}); err != nil {
        return fmt.Errorf("failed to delete network policy %s in namespace %s: %w", policyName1, workload1.Namespace, err)
    }
    if err := clientset.NetworkingV1().NetworkPolicies(workload2.Namespace).Delete(context.TODO(), policyName2, metav1.DeleteOptions{}); err != nil {
        return fmt.Errorf("failed to delete network policy %s in namespace %s: %w", policyName2, workload2.Namespace, err)
    }

    return nil
}
