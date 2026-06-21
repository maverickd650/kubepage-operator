# kubepage-operator

A Kubernetes operator that turns [gethomepage/homepage](https://github.com/gethomepage/homepage)
into a set of CRDs: define your dashboard's settings, services, bookmarks, and
header widgets as Kubernetes objects, and the operator renders them into
homepage's config files and runs the homepage Deployment for you.

It wraps the upstream homepage image rather than reimplementing it — the
operator owns the Kubernetes side (Deployment, ConfigMap, Service, optional
Ingress, secret delivery) while homepage itself renders the dashboard. See
[`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md) for the full design
rationale (kept local-only; ask in-repo if you need a copy).

## CRDs

| Kind | Renders into | Purpose |
|------|---------------|---------|
| `Instance` (`pageinst`) | Deployment, ConfigMap, Service, optional Ingress | The homepage deployment itself. Every other CRD names one via `instanceRef`. |
| `Configuration` (`pcfg`) | `settings.yaml` | Theme, color, layout, and other dashboard-wide settings. One per Instance. |
| `ServiceEntry` (`psvc`) | `services.yaml` | One service card (with optional widgets) in a named group. |
| `Bookmark` (`pbmk`) | `bookmarks.yaml` | One bookmark link in a named group. |
| `InfoWidget` (`piw`) | `widgets.yaml` | One header widget (resources, search, datetime, weather, kubernetes, ...). |

Every config CRD (`Configuration`, `ServiceEntry`, `Bookmark`, `InfoWidget`)
carries an `instanceRef.name` naming the `Instance` it belongs to, and any
namespace-matching is implicit: they must live in the same namespace as that
`Instance`. Secret-bearing fields (a `ServiceEntry` widget's API key, an
`InfoWidget`'s API key, etc.) use a `secretKeyRef` rather than an inline
value; the operator mounts the referenced Secret as a file into the homepage
pod and substitutes a `{{HOMEPAGE_FILE_*}}` placeholder, so credentials never
appear in pod env or in the rendered ConfigMap.

## Quickstart

```sh
# Install the CRDs
make install

# Deploy the controller (build/push your own image, or use an already-published one)
make deploy IMG=<some-registry>/kubepage-operator:tag

# Apply the sample Instance plus one of every config CRD
kubectl apply -k config/samples/
```

The samples under [`config/samples/`](config/samples/) show the minimal shape
of every CRD: [`Instance`](config/samples/page_v1alpha1_instance.yaml),
[`Configuration`](config/samples/page_v1alpha1_configuration.yaml),
[`ServiceEntry`](config/samples/page_v1alpha1_serviceentry.yaml),
[`Bookmark`](config/samples/page_v1alpha1_bookmark.yaml), and
[`InfoWidget`](config/samples/page_v1alpha1_infowidget.yaml). Once applied,
`kubectl get pageinst,pcfg,psvc,pbmk,piw` shows their `Ready` status and
bound counts; the homepage Service is reachable by port-forwarding it
(`kubectl port-forward svc/instance-sample 3000:3000`) or by setting
`spec.ingress.enabled: true` on the `Instance` to expose it via an Ingress.

### To Uninstall

```sh
kubectl delete -k config/samples/
make uninstall   # removes the CRDs
make undeploy     # removes the controller
```

## Development

### Prerequisites
- go version v1.24.6+
- docker version 17.03+
- kubectl version v1.11.3+
- Access to a Kubernetes v1.11.3+ cluster

### Build and run

```sh
make docker-build docker-push IMG=<some-registry>/kubepage-operator:tag
make deploy IMG=<some-registry>/kubepage-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself
> cluster-admin privileges or be logged in as admin.

After editing `*_types.go` or `+kubebuilder` markers, regenerate CRDs/RBAC and
DeepCopy methods, then lint and test:

```sh
make manifests generate
make lint-fix test
```

See [`AGENTS.md`](AGENTS.md) for the full kubebuilder mechanics this project
follows (project structure, never-hand-edit files, RBAC marker conventions).

## Project Distribution

### As a YAML bundle (Kustomize)

```sh
make build-installer IMG=<some-registry>/kubepage-operator:tag
```

This generates `dist/install.yaml`, containing every resource needed to
install the project (CRDs, RBAC, Deployment) with no other dependencies:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/kubepage-operator/<tag or branch>/dist/install.yaml
```

### As a Helm chart

A Helm chart packaging the CRDs and controller lives under
[`dist/chart`](dist/chart). To install it:

```sh
helm install kubepage-operator ./dist/chart --namespace kubepage-operator-system --create-namespace
```

If you change the project's API, RBAC, or manager manifests, regenerate the
chart:

```sh
kubebuilder edit --plugins=helm/v2-alpha --force
```

**NOTE:** `--force` overwrites `dist/chart`; re-apply any custom values you
had in `dist/chart/values.yaml` or `dist/chart/manager/manager.yaml`
afterwards.

## Contributing

Issues and PRs welcome. Run `make help` for the full list of `make` targets,
and see [`AGENTS.md`](AGENTS.md) before touching generated files or CRD
markers.

More information can be found via the
[Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html).

## License

// TODO: a LICENSE file has not been added to this repository yet.
