# kubepage-operator

A Kubernetes operator that serves a small, native dashboard (Go + htmx, a
single binary, no Node/React build step) for a curated set of self-hosted
services — Plex, Stash, Paperless-ngx, Grafana, Prometheus, UniFi, TrueNAS,
Cloudflared, Linkwarden, Home Assistant, Mealie — driven entirely by CRDs.
Define services, bookmarks, and dashboard look/settings as Kubernetes
objects, and the operator runs a per-Instance dashboard Deployment that reads
those CRDs directly and polls each service's API on an interval.

The dashboard process resolves any Secret-backed credentials (a `ServiceEntry`
widget's API key, etc.) itself, in-process — the plaintext value never
appears in pod env, a ConfigMap, or a projected file; it only ever exists in
the dashboard pod's memory for the duration of the poll. See
[`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md) for the full design
rationale (kept local-only; ask in-repo if you need a copy).

## CRDs

| Kind | Purpose |
|------|---------|
| `Instance` (`pageinst`) | The dashboard Deployment, Service, optional Ingress, and the per-Instance ServiceAccount/Role/RoleBinding the dashboard pod runs as. Every other CRD names one via `instanceRef`. |
| `Configuration` (`pcfg`) | Title, description, favicon, theme, color, background, card blur, header style, default link target, and the header search box. One per Instance. |
| `ServiceEntry` (`psvc`) | One service card (with optional widgets polling that service's API) in a named group. Supports an HTTP `ping`/`siteMonitor` up/down status, per-card link `target`, and `showStats`/`hideErrors` toggles. |
| `Bookmark` (`pbmk`) | One static bookmark link in a named group. |
| `InfoWidget` (`piw`) | One header-strip widget: `datetime` (client-side clock), `greeting` (static text), or `openmeteo` (current weather). |

Every config CRD (`Configuration`, `ServiceEntry`, `Bookmark`, `InfoWidget`)
carries an `instanceRef.name` naming the `Instance` it belongs to, and any
namespace-matching is implicit: they must live in the same namespace as that
`Instance`.

### Admission validation

Beyond the CRD schemas, the operator ships
[`ValidatingAdmissionPolicies`](config/admission/secret_source_policy.yaml)
(CEL, no webhook server or certificates to manage) that reject invalid configs
at apply time. Currently they enforce that every secret-bearing field
(`SecretValueSource`) sets exactly one of `value` or `secretKeyRef`, so a
missing or ambiguous credential surfaces as a `kubectl apply` error rather than
a broken widget card at poll time. These require **Kubernetes v1.30+**
(`ValidatingAdmissionPolicy` is GA from 1.30); on the Helm chart they can be
turned off with `--set admissionPolicies.enabled=false`.

### Exposing the dashboard

Every `Instance` always gets a ClusterIP `Service`. To expose it beyond the
cluster, set one of:

- `spec.ingress` — a classic `networking.k8s.io/v1` `Ingress` (`enabled`,
  `host`, `ingressClassName`, `annotations`, `tls.secretName`).
- `spec.gateway` — a Gateway API `HTTPRoute` (`enabled`, `hostnames`,
  `parentRef.{name,namespace,sectionName}`, `annotations`), attached to a
  `Gateway` you manage separately. Only takes effect if the cluster actually
  has Gateway API CRDs installed; the manager checks once at startup
  (`kubectl logs` shows `Gateway API support enabled=...`), and an `Instance`
  with `spec.gateway.enabled: true` on a cluster without them reports a clear
  `Available=False` condition rather than the manager crashing.

Both can be set at once (e.g. Ingress for one hostname, Gateway API for
another); neither is required if you're reaching the dashboard via
port-forward or your own externally-managed routing.

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
bound counts; the dashboard Service is reachable by port-forwarding it
(`kubectl port-forward svc/instance-sample 8080:8080`) or by setting
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
