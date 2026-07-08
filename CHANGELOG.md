# Changelog

## [0.2.0](https://github.com/maverickd650/kubepage-operator/compare/v0.1.0...v0.2.0) (2026-07-08)


### ⚠ BREAKING CHANGES

* Instance, Configuration, and ServiceEntry CRDs are renamed to Dashboard, DashboardStyle, and ServiceCard respectively. Existing manifests must be updated: kind, dashboardRef (was instanceRef), collapse/indexing/errorDisplay/internetSearchEntry/ visitURLEntry/scope (renamed enums), and replicas (was size). A DashboardStyle's metadata.name must now equal its dashboardRef.name.
* **api:** LayoutGroupSpec.{Header,InitiallyCollapsed,UseEqualHeights}, ConfigurationSpec.{FullWidth,DisableCollapse,GroupsInitiallyCollapsed, UseEqualHeights,DisableIndexing}, SearchSpec.FilterCards, HighlightRuleSpec.{Negate,CaseSensitive}, FieldHighlight.ValueOnly, ServiceEntrySpec.{ShowStats,HideErrors}, IngressSpec.Enabled, GatewaySpec.Enabled, and InstanceSpec.HostUsers changed from CRD schema type boolean to string. Existing stored CRs setting any of these fields will fail validation on next apply/update until migrated to the new enum values (e.g. `true` -> "Enabled", `false` -> "Disabled").
* **container:** Update image ghcr.io/devcontainers/features/docker-in-docker (3 → 4) ([#32](https://github.com/maverickd650/kubepage-operator/issues/32))

### Features

* add project automation skills, session-start hook, and CI guards ([#72](https://github.com/maverickd650/kubepage-operator/issues/72)) ([7bb4134](https://github.com/maverickd650/kubepage-operator/commit/7bb413452d753c723069433a043dc2f9229d6c73))
* **api:** harden CRD validation and convert bool fields to string enums ([#54](https://github.com/maverickd650/kubepage-operator/issues/54)) ([5b907ec](https://github.com/maverickd650/kubepage-operator/commit/5b907ec9d6b343c7aab753cb9b437f1ab7a3ffff))
* **container:** Update image ghcr.io/devcontainers/features/docker-in-docker (3 → 4) ([#32](https://github.com/maverickd650/kubepage-operator/issues/32)) ([6745081](https://github.com/maverickd650/kubepage-operator/commit/674508136099872a3edee0099d11316f3282e3be))
* **controller:** reflect Deployment readiness and add per-step failure reasons ([#52](https://github.com/maverickd650/kubepage-operator/issues/52)) ([ce919c1](https://github.com/maverickd650/kubepage-operator/commit/ce919c13fd5555836248e5ad60881934b464d61a))
* **dashboard:** add header widget icons, usage bars, and threshold highlights ([#38](https://github.com/maverickd650/kubepage-operator/issues/38)) ([c1a091c](https://github.com/maverickd650/kubepage-operator/commit/c1a091c74f5b9f9860e38bcff3fb41694a522275))
* **dashboard:** close homepage gap-analysis Phase 1 and Phase 2 ([#60](https://github.com/maverickd650/kubepage-operator/issues/60)) ([7e5358c](https://github.com/maverickd650/kubepage-operator/commit/7e5358cc98ace2ad3d1a9801be39785108d498bc))
* **dashboard:** close homepage gap-analysis Phase 3 and Phase 4 ([#62](https://github.com/maverickd650/kubepage-operator/issues/62)) ([997ae82](https://github.com/maverickd650/kubepage-operator/commit/997ae822052492ec02d33b437653e3295243acff))
* **dashboard:** pod-health monitoring, field highlighting, and homepage UI parity ([#47](https://github.com/maverickd650/kubepage-operator/issues/47)) ([3b2f844](https://github.com/maverickd650/kubepage-operator/commit/3b2f8448f19026b1dea170c1dd2c9233421ab84c))
* **deps:** update dependency kubeconform (0.6.7 → 0.8.0) ([#78](https://github.com/maverickd650/kubepage-operator/issues/78)) ([e87c191](https://github.com/maverickd650/kubepage-operator/commit/e87c191e40af3f2708764c664a53fb67eacb5e56))
* implement docs/security-review.md hardening recommendations ([#64](https://github.com/maverickd650/kubepage-operator/issues/64)) ([f908c7a](https://github.com/maverickd650/kubepage-operator/commit/f908c7a019d6d3a4af83bdbd358a9ef05b49ed48))
* **preview:** add --sample-data mode with per-widget placeholder data ([#87](https://github.com/maverickd650/kubepage-operator/issues/87)) ([76de8d6](https://github.com/maverickd650/kubepage-operator/commit/76de8d6fd45a88678b24600ecbf9b43a674081c1))
* **preview:** add local dashboard preview subcommand ([#81](https://github.com/maverickd650/kubepage-operator/issues/81)) ([4da7106](https://github.com/maverickd650/kubepage-operator/commit/4da710625af5eebfedc9f6b00b81db249bb954ac))
* rename CRD kinds and adopt CEL schema validation ([#70](https://github.com/maverickd650/kubepage-operator/issues/70)) ([9ba02af](https://github.com/maverickd650/kubepage-operator/commit/9ba02af3797fa9b32bff66cd2da92f4214dbac03))


### Bug Fixes

* address code review findings across CRDs, dashboard, and controller ([#46](https://github.com/maverickd650/kubepage-operator/issues/46)) ([409e294](https://github.com/maverickd650/kubepage-operator/commit/409e294dd882d53ca6f16039eddf16f8ab0e2eed))
* **dashboard:** escape card/bookmark names in quick-launch palette ([#40](https://github.com/maverickd650/kubepage-operator/issues/40)) ([7476423](https://github.com/maverickd650/kubepage-operator/commit/74764231b914777b6c348c7653c6860a54c2653d))
* **dashboard:** harden HTTP surface, fix RBAC/SSRF gaps, improve poller performance ([#58](https://github.com/maverickd650/kubepage-operator/issues/58)) ([3696092](https://github.com/maverickd650/kubepage-operator/commit/369609247beb8874b7e41d8028dc22bbf4866fd3))
* **dashboard:** prevent style-tag breakout via Background.Image ([#39](https://github.com/maverickd650/kubepage-operator/issues/39)) ([f389a7c](https://github.com/maverickd650/kubepage-operator/commit/f389a7cda9d6a833bf5d48dc67f5b475250618a0))
* **deps:** update dependency go (1.26.4 → 1.26.5) ([#88](https://github.com/maverickd650/kubepage-operator/issues/88)) ([0cef2ed](https://github.com/maverickd650/kubepage-operator/commit/0cef2ed38ede79006f9ce978747c74c000e24213))


### Performance Improvements

* **dashboard:** reduce polling overhead and close rendering gaps vs. homepage ([#68](https://github.com/maverickd650/kubepage-operator/issues/68)) ([e2e7a25](https://github.com/maverickd650/kubepage-operator/commit/e2e7a25d20d4f3c39840ea294e0b92c4262f4f53))

## [0.1.0](https://github.com/maverickd650/kubepage-operator/compare/v0.0.1...v0.1.0) (2026-06-22)


### Features

* dashboard hardening, scoped Secret RBAC, widget-type validation ([#12](https://github.com/maverickd650/kubepage-operator/issues/12)) ([9f62bc1](https://github.com/maverickd650/kubepage-operator/commit/9f62bc1ba21e44bde817999c51eb9fe437d1889e))
* **dashboard:** add tabs/layout model for ServiceEntry groups ([#15](https://github.com/maverickd650/kubepage-operator/issues/15)) ([7c63333](https://github.com/maverickd650/kubepage-operator/commit/7c6333310612bee8d4145f496ed67f7c1066e113))
* initial release of kubepage-operator ([dad2e7b](https://github.com/maverickd650/kubepage-operator/commit/dad2e7b1db1c7d1790fae2e3df5f628a60344207))
* visual refresh toward homepage look-and-feel ([#16](https://github.com/maverickd650/kubepage-operator/issues/16)) ([4122a21](https://github.com/maverickd650/kubepage-operator/commit/4122a21a725dcc7436bf62a1ef110b601fc69b94))


### Bug Fixes

* create license ([#14](https://github.com/maverickd650/kubepage-operator/issues/14)) ([f72bda7](https://github.com/maverickd650/kubepage-operator/commit/f72bda7ec2080fa1050a58e61c2d16300c925bc6))
* use mise tasks instead of make in e2e test setup ([#17](https://github.com/maverickd650/kubepage-operator/issues/17)) ([e8c6b29](https://github.com/maverickd650/kubepage-operator/commit/e8c6b295a5aec1766526f4a02d1de6f43a5f4cb0))
