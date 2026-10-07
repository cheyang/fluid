// refcheck proves whether the image references rendered by the code under review are
// parseable container image references, using github.com/distribution/reference - the
// parser Kubernetes' kubelet uses (k8s.io/kubernetes/pkg/util/parsers wraps it).
package main

import (
	_ "crypto/sha256" // register sha256 so go-digest can validate the digest bytes
	"fmt"

	"github.com/distribution/reference"
)

func main() {
	candidates := []string{
		// what the code under review renders for {image: "btxu/mooncake@sha256:..."},
		// completed with template tag "v1", via image + ":" + imageTag:
		"btxu/mooncake@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552:v1",
		// controls:
		"btxu/mooncake@sha256:067614b70d25b496e3edc3480747d558ee8a364ef47a67f669f5d96ca5098552", // the digest alone
		"btxu/mooncake:v1",                // the template image
		"fluid/cache:v2",                  // a completed tag-only version
		"registry.local:5000/mooncake:v3", // registry port + tag
	}
	for _, c := range candidates {
		_, err := reference.ParseNormalizedNamed(c)
		if err != nil {
			fmt.Printf("INVALID %q -> %v\n", c, err)
		} else {
			fmt.Printf("VALID   %q\n", c)
		}
	}
}
