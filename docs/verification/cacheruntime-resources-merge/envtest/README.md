# envtest proof of the apiserver rules behind F6/F9

Standalone module (own go.mod, deliberately outside the fluid build). Run:

```bash
setup-envtest use 1.29.0 -p path   # or any 1.2x assets
KUBEBUILDER_ASSETS=<path> go test -count=1 -v .
```

Observed (results/round3/envtest-apiserver-rule.txt): a real API server rejects a pod
with hugepages-2Mi request 1Gi != limit 2Gi ("must be equal to hugepages-2Mi limit of 2Gi")
and one with cpu request 1 > limit 500m ("must be less than or equal to cpu limit"), and
accepts the equal hugepages control. This is the rejection the PR's validator must predict.
