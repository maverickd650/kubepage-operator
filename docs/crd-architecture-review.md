# CRD architecture review

Reviewed at commit `e66bfc4` (2026-07). Scope: the five CRDs in
`api/v1alpha1/` (`Instance`, `Configuration`, `ServiceEntry`, `Bookmark`,
`InfoWidget`), the CEL admission policies in `config/admission/`, and how the
controllers (`internal/controller/`) and the dashboard process
(`internal/dashboard/`) consume them. This is a recommendations plan, not a
set of applied changes. The project is unreleased, so breaking renames are
still cheap — several suggestions below only make sense *because* nothing
depends on the current names yet.

Constraint honored throughout: no suggestion below introduces new
pointer-typed fields; where a pattern choice exists, the pointer-free variant
is recommended.

## What is already in good shape

- **The overall shape is right.** One workload CRD (`Instance`) plus small,
  per-card config CRDs bound by `instanceRef`, with the dashboard reading
  config live instead of the controller re-rendering, is a clean split. The
  thin ref-validating reconcilers (`serviceentry_controller.go`,
  `configuration_controller.go`, ...) with `Watches` on `Instance` for
  out-of-order-apply self-healing are exactly the standard pattern.
- **Validation discipline is unusually good for an alpha API**: nearly every
  field carries `MinLength`/`MaxLength`/`Pattern`/`Enum`/`Minimum` bounds,
  lists carry `listType` and `MaxItems`, maps carry `MinProperties`.
- **String enums instead of booleans** (`Enabled`/`Disabled` etc.) follow the
  Kubernetes API conventions' own advice ("think twice about `bool` fields —
  many ideas start as boolean but evolve"). Keep this; the issues below are
  about *which* words, not about the pattern.
- `SecretValueSource` + in-process resolution, the `instanceRef`
  same-namespace rule, deliberate field-by-field drift comparison, and the
  documented RBAC caveat on `widgets.secrets` are all sound and documented
  where they live.

## Findings

### 1. `Instance.spec.hostUsers` documentation contradicts the implementation (bug)

`api/v1alpha1/instance_types.go` says:

> hostUsers controls whether the container's user namespace is separate from
> the host. Defaults to "Enabled" (separate namespace).

But `hostUsersBool` (`internal/controller/instance_controller.go`) maps
`Enabled` → pod `hostUsers: true`, and in `corev1.PodSpec`, `hostUsers: true`
(the Kubernetes default) means the pod **uses the host's** user namespace;
`false` is what requests a **separate** one. So with the marker default
`+default="Enabled"`, the field's doc promises isolation while the generated
pod explicitly opts out of it. The existing test asserts the implementation
side (`HostUsers: Disabled` → pod `hostUsers: false`), so it's the comment —
and possibly the chosen default — that's wrong.

Decide which semantic was intended and fix the other half:

- If the field should mirror `corev1.PodSpec.HostUsers` (least surprising,
  given the name): keep the mapping, fix the doc comment to "Enabled (the
  default) runs the pod in the host's user namespace, matching the Kubernetes
  default; Disabled requests a separate user namespace."
- If the intent was isolation-by-default: keep the doc, invert the default
  and/or the mapping — but then rename the field (e.g. `userNamespace:
  Isolated|Host`) so it no longer shares a name with a pod field it inverts.

Either way, this needs a test-locked decision before first release; it's a
security-posture field.

### 2. Move cross-object-file validation into the CRD schema (retire two of three VAPs)

The three `ValidatingAdmissionPolicy` files in `config/admission/` enforce
invariants that current CRD machinery can express *inside the schema*, which
is strictly more robust: the rules travel with the CRD (no way to install the
CRDs but forget the policies — today that's a silent validation gap), they
show up in `kubectl explain`/OpenAPI/docs generators, they're exercised by
every envtest that applies the CRDs, and the cluster floor drops from v1.30
(VAP GA) to v1.29 (CRD CEL validation GA). Ratcheting validation (GA in
recent Kubernetes, and go.mod is already on k8s.io/api v0.36) makes later
tightening safe for pre-existing objects.

Concretely:

- **`secret_source_policy.yaml`** → one `XValidation` marker on the shared
  struct in `common_types.go`, replacing both per-resource policies:

  ```go
  // +kubebuilder:validation:XValidation:rule="(has(self.value) ? 1 : 0) + (has(self.secretKeyRef) ? 1 : 0) == 1",message="exactly one of value or secretKeyRef must be set"
  type SecretValueSource struct { ... }
  ```

- **`serviceentry_monitor_source_policy.yaml`** → an `XValidation` marker on
  `ServiceEntrySpec`:

  ```go
  // +kubebuilder:validation:XValidation:rule="(has(self.ping) ? 1 : 0) + (has(self.siteMonitor) ? 1 : 0) + (has(self.podSelector) ? 1 : 0) <= 1",message="at most one of ping, siteMonitor, or podSelector may be set"
  ```

- **`widget_type_policy.yaml`** → plain `+kubebuilder:validation:Enum`
  markers on `ServiceWidget.Type` and `InfoWidgetSpec.Type`. The "keep in
  sync with the dashboard registry" burden is identical either way (both the
  VAP list and an enum are hand-maintained in this repo and shipped
  together); the existing sync tests (`widget_type_policy_test.go`) can
  assert registry == enum instead of registry == policy. An enum additionally
  fixes the stale examples in `InfoWidgetSpec.Type`'s doc comment ("e.g.
  resources, search" — neither is an allowed type).

Once in the schema, a few more invariants that today only fail at poll time
become one-line CEL rules:

- `SearchSpec`: `provider == "custom"` requires `url`
  (`self.provider != 'custom' || has(self.url)` — note `provider` defaults,
  so `has()` guards aren't needed after defaulting).
- `HighlightRuleSpec`: `between`/`outside` require `value2`
  (`!(self.when in ['between','outside']) || has(self.value2)`).
- `ServiceWidget`: every type except the cluster-sourced ones requires `url`.

Keep `config/admission/` only if you actively want to vary policy without
re-applying CRDs; nothing in the current design needs that.

### 3. Kind names collide with widely deployed CRDs (rename while it's free)

Kinds are only unique per group, but `kubectl get <kind>` resolution and
human communication are not:

- **`ServiceEntry`** is one of Istio's best-known CRDs
  (`serviceentries.networking.istio.io`). On any mesh-enabled cluster,
  `kubectl get serviceentry` becomes ambiguous and every doc/blog/Slack
  mention of "a ServiceEntry" needs qualification.
- **`Configuration`** collides with Knative Serving
  (`configurations.serving.knative.dev`), among others.
- **`Instance`** is generic to the point of meaninglessness in conversation
  ("create an Instance" of what?).

Since nothing is released, renaming is a mechanical sweep. A coherent set
that keeps the existing short names workable:

| Current | Suggested | Rationale |
|---|---|---|
| `Instance` | `Dashboard` | Says what the workload *is*; `kubectl get dashboard` reads naturally. |
| `Configuration` | `DashboardStyle` (or fold into `Dashboard`, see #4) | Unambiguous, describes content (theme/layout/search). |
| `ServiceEntry` | `ServiceCard` | Avoids Istio; matches the UI concept ("card") already used throughout the docs/comments. |
| `Bookmark` | `Bookmark` | Low collision risk; fine as is. |
| `InfoWidget` | `InfoWidget` | Fine as is. |

The group `page.kubepage.dev` is fine (group-qualified names stay unique);
this is purely about kind ergonomics.

### 4. The Configuration-per-Instance singleton is unenforced and ties break silently

CLAUDE.md says "One per Instance", but nothing enforces it:
`LoadSite` picks the lexicographically first bound `Configuration`
(`site_test.go` locks this in), the losers keep `Available=True`, and
`Instance.status.boundConfigurations` just counts. A user with two bound
Configurations gets a silently half-applied theme and no signal.

Options, in increasing order of change:

1. **Surface it** (minimum): set `Available=False` /
   `reason=MultipleConfigurationsBound` on every bound Configuration except
   the winner, and a warning condition on the Instance when
   `boundConfigurations > 1`.
2. **Make the name the binding** (recommended): require
   `metadata.name == spec.instanceRef.name` via a root-level CEL rule
   (`self.metadata.name` is accessible at the object root):

   ```go
   // +kubebuilder:validation:XValidation:rule="self.metadata.name == self.spec.instanceRef.name",message="a Configuration must be named after the Instance it configures"
   ```

   The API server's name uniqueness then makes a second bound Configuration
   *impossible*, `kubectl get pcfg <instance-name>` becomes the obvious
   lookup, and `LoadSite`'s list-and-sort becomes a direct `Get`. (At that
   point `instanceRef` on Configuration is technically redundant; keeping it
   explicit-but-validated is fine and stays consistent with the other kinds.)
3. **Fold `ConfigurationSpec` into `Instance.spec`**: removes the CRD and the
   problem, at the cost of a fatter Instance and losing the ability to grant
   look-and-feel editing without Deployment-shaped RBAC. Not recommended —
   the RBAC split is worth keeping — but worth stating as considered.

### 5. Enum vocabulary: double negatives and name/value mismatches

The `Enabled`/`Disabled` pattern is good, but several fields combine a
negated field name with a polarity enum, or repeat one enum value as the
field name. Each is a small paper cut; together they make specs hard to read
without the reference docs open:

| Field | Today reads as | Suggestion |
|---|---|---|
| `Configuration.disableCollapse: Enabled` | "disabling is enabled" (double negative) | `collapse: Enabled\|Disabled` |
| `Configuration.disableIndexing: Indexed\|NoIndex` | field is a verb, values are states — `disableIndexing: Indexed` means indexing *on* | rename field to `indexing`, keep values |
| `Configuration.hideErrors: Show\|Hide` / `ServiceEntry.hideErrors` | `hideErrors: Show` = don't hide | `errorDisplay: Shown\|Hidden` (or `errors:`) |
| `SearchSpec.hideInternetSearch`, `hideVisitURL: Enabled\|Disabled` | "hiding enabled" | `internetSearchEntry` / `visitURLEntry: Shown\|Hidden` |
| `FieldHighlight.valueOnly: WholeField\|ValueOnly` | field name repeats one value; `valueOnly: WholeField` is self-contradictory | `scope: WholeField\|ValueOnly` |
| `HighlightRuleSpec.negate: Match\|Negate` | same shape (`negate: Match`) | `matchPolarity:` or just accept it, lowest priority |

Rule of thumb worth adopting in CONTRIBUTING/AGENTS docs: **field names are
affirmative nouns for the thing being controlled; enum values are states of
that thing; a value never repeats the field name.**

Related consistency nit: enum casing is mixed — operator-native toggles are
PascalCase (`Enabled`, `Shown`) while homepage-mirroring enums are lowercase
(`dot`, `basic`, `light`, `dark`, `good`, `row`, `underlined`). That split is
defensible (lowercase = inherited homepage vocabulary), but it's currently
implicit; write the rule down or normalize to PascalCase per upstream API
conventions.

### 6. Spec ergonomics on `Instance`

- **`containerPort` is required** — and it's the *only* required field.
  It's an implementation detail of the operand (the operator controls both
  ends of that port), so forcing every manifest to pick one is boilerplate.
  Give it `+default=8080` and make it optional; a minimal
  `kind: Instance` + `spec: {}` (or near it) is a meaningful first-run UX
  win for a homelab-facing project.
- **`size` → `replicas`**: `replicas` is the ecosystem-wide word
  (Deployment, StatefulSet, `kubectl scale`). If multi-replica stays
  discouraged (per the excellent doc comment about per-replica polling),
  that's an argument for *documenting* it, not for a nonstandard name. If
  it's ever meant to be scaled, `replicas` also unlocks the
  `+kubebuilder:subresource:scale` marker (`kubectl scale`, HPA) — and if
  it's deliberately *not*, note that in the comment so nobody adds the
  subresource later without reading it.
- **Missing scheduling knobs**: no `nodeSelector`, `tolerations`,
  `affinity`, `topologySpreadConstraints`, `imagePullSecrets`, or
  `priorityClassName`. For the stated audience (homelabs: mixed-arch nodes,
  tainted Pis, single-node control planes) `nodeSelector` and `tolerations`
  in particular will be asked for immediately. All are plain `corev1` types
  passed through to the pod template, same as the existing
  probe/securityContext fields.
- **No Service customization**: `serviceForInstance` hardcodes a ClusterIP
  Service, while the `ingress` doc comment itself says most users will reach
  the dashboard via "port-forward, LoadBalancer, existing Ingress" — but the
  CRD offers no way to make the Service a LoadBalancer or annotate it (e.g.
  for MetalLB/Tailscale). A small optional stanza mirrors the existing
  ingress/gateway shape:

  ```yaml
  service:
    type: LoadBalancer        # enum: ClusterIP|LoadBalancer|NodePort, default ClusterIP
    annotations: { ... }
  ```

### 7. Schemaless widget config is the right call today — document its edge and leave a union path open

`ServiceWidget.Config` / `InfoWidgetSpec.Options` as
`apiextensionsv1.JSON` + `PreserveUnknownFields` is a pragmatic fit for 14+
widget types, and the registry pattern ("new widget = one file, no
poller/server changes") is worth protecting. Two follow-ups:

- **The typo problem is real and invisible**: `config: {querry: ...}` is
  accepted, preserved, and silently ignored until the card errors (or
  doesn't). At minimum, document per-widget config keys in `kubectl
  explain`-visible field docs; better, have widgets reject unknown keys at
  poll time with an explicit field error so the typo is at least *loud*.
- **If/when the widget set stabilizes**, the modern endgame is a
  discriminated union: keep `type` as the discriminator, add one optional
  typed struct per widget (`plex:`, `grafana:`, ...), and one CEL rule
  asserting the set struct matches `type`. That's a big surface (and a
  breaking change), so it's a v1beta1 decision, not now — but avoid building
  anything in the meantime that assumes config stays opaque (e.g. don't
  start reaching into the JSON from the controller).

Minor: `apiextensionsv1.JSON` drags `k8s.io/apiextensions-apiserver` into the
API package's dependency graph for anyone importing `api/v1alpha1` as a
module. It's already an indirect dep here, so this is only worth revisiting
if the API package is ever split out for consumers.

### 8. Smaller items

- **Optional-enum field style is inconsistent**: `DiscoverySpec.Enabled` is a
  plain `string` with `+default`, while its sibling `HomepageCompat` (and
  most enums elsewhere) are `*string`. The plain-`string` +
  `omitempty` + `+default` variant is the pointer-free pattern and reads
  better in Go (`if spec.Enabled == Enabled` vs nil-check-then-deref);
  standardizing on it for enums whose zero value can mean "unset" would also
  shrink a lot of `ptr.To(...)` in tests. (Numeric optionals where `0` is a
  legal value, e.g. `BackgroundSpec.Saturate`, genuinely need the pointer —
  leave those.)
- **Defaults live in two places**: some documented defaults are schema
  markers (`target: +default="_blank"`), others are code-side ("Defaults to
  'dot' when unset here too" — `statusStyle` has no marker). Schema defaults
  are visible in `kubectl get -o yaml` and `explain`; code defaults are
  invisible. Pick per-field deliberately, but today the split looks
  accidental — an audit pass would be cheap.
- **`observedGeneration`**: only `InstanceStatus` has the top-level field.
  The conditions set via `meta.SetStatusCondition` should also populate
  `ObservedGeneration` on each condition (pass `obj.Generation` when
  building them in `conditions.go`) so `kubectl wait
  --for=condition=Available` can't be satisfied by a stale condition.
- **`ping`/`siteMonitor`/widget `url` allow `http://` for secret-bearing
  targets**: already in scope of `docs/security-review.md`; noting here only
  because a CEL rule (e.g. warn-level VAP or a documented stance) is the
  natural companion to finding #2.
- **`IngressSpec.Enabled`/`GatewaySpec.Enabled` vs. stanza presence**: the
  presence of `spec.ingress` could itself mean "on" (the Gateway API style),
  making the inner toggle redundant. Keeping the explicit toggle is
  defensible (lets users park config while disabled) — this is a
  consciously-fine, not a fix.

## Suggested order of attack

1. **#1** (`hostUsers` doc/default contradiction) — a correctness/security
   decision; smallest diff, highest urgency.
2. **#3 + #5** (kind renames + enum vocabulary) — breaking renames get
   strictly more expensive with every passing week of an unreleased project.
3. **#2** (schema-embedded CEL + enums, retire VAPs) — mechanical, deletes
   installed surface, drops the cluster floor to v1.29.
4. **#4** (Configuration singleton via name binding).
5. **#6** (`containerPort` default, `replicas`, scheduling + service knobs)
   — additive, non-breaking, can trail the renames.
6. **#7/#8** as background chores.
