// Verification harness helper (review artifact, not production code).
// Starts an envtest control plane (kube-apiserver + etcd) and writes a
// kubeconfig file that helm/kubectl can use, then blocks until SIGTERM.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: envtest-kubeconfig <output-kubeconfig-path>")
		os.Exit(2)
	}
	out := os.Args[1]

	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start envtest: %v\n", err)
		os.Exit(1)
	}

	kc := clientcmdapi.NewConfig()
	kc.Clusters["envtest"] = &clientcmdapi.Cluster{
		Server:                   cfg.Host,
		CertificateAuthorityData: cfg.TLSClientConfig.CAData,
	}
	kc.AuthInfos["envtest"] = &clientcmdapi.AuthInfo{
		ClientCertificateData: cfg.TLSClientConfig.CertData,
		ClientKeyData:         cfg.TLSClientConfig.KeyData,
	}
	kc.Contexts["envtest"] = &clientcmdapi.Context{Cluster: "envtest", AuthInfo: "envtest"}
	kc.CurrentContext = "envtest"
	if err := clientcmd.WriteToFile(*kc, out); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write kubeconfig: %v\n", err)
		os.Exit(1)
	}

	// readiness marker
	fmt.Printf("ENVTEST_READY %s\n", out)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	// give the apiserver a moment, then stop
	time.Sleep(200 * time.Millisecond)
	_ = env.Stop()
}
