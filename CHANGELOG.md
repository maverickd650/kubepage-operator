# Changelog

## [0.3.1](https://github.com/maverickd650/kubepage-operator/compare/v0.3.0...v0.3.1) (2026-07-12)


### Bug Fixes

* **chart:** default manager image to the published ghcr.io repository ([#150](https://github.com/maverickd650/kubepage-operator/issues/150)) ([41778f7](https://github.com/maverickd650/kubepage-operator/commit/41778f7a88d9ff196af0994b9df758627b408416))

## [0.3.0](https://github.com/maverickd650/kubepage-operator/compare/v0.2.0...v0.3.0) (2026-07-12)


### ⚠ BREAKING CHANGES

* **dashboard:** switch grafana widget to admin/stats, matching gethomepage/homepage ([#121](https://github.com/maverickd650/kubepage-operator/issues/121))
* **api:** Bookmark.spec.name/href/target/order/abbr/icon/description, ServiceCard.spec.name/href/target/icon/description/showStats/errorDisplay/ ping/siteMonitor/podSelector/statusStyle/widgets/order, and InfoWidget.spec.type/url/order/icon/align/secrets/caCert/options/ pollIntervalSeconds no longer exist. bookmarks/services/widgets are now +required. Existing single-entry objects must be rewritten to set the list field with one entry.

### Features

* **api:** add multi-bookmark form to Bookmark ([#101](https://github.com/maverickd650/kubepage-operator/issues/101)) ([cfbe929](https://github.com/maverickd650/kubepage-operator/commit/cfbe929d3289f9cdf1b57ae7a1dee360f1e943b9))
* **api:** add multi-card services form to ServiceCard ([#92](https://github.com/maverickd650/kubepage-operator/issues/92)) ([4c11dd4](https://github.com/maverickd650/kubepage-operator/commit/4c11dd4089e036bad69c9c582166da843a9b30c1))
* **api:** add multi-widget form to InfoWidget ([#102](https://github.com/maverickd650/kubepage-operator/issues/102)) ([ac0533d](https://github.com/maverickd650/kubepage-operator/commit/ac0533d6c4eb5b644f72dd0a35e8117f8ff8df78))
* **api:** cross-namespace discovery, volumes, status.url, bookmark scheme allowlist ([#131](https://github.com/maverickd650/kubepage-operator/issues/131)) ([c2ca48a](https://github.com/maverickd650/kubepage-operator/commit/c2ca48ae5d538d24380102b19c00ebe210459c68))
* **api:** multi-form status feedback + small API-surface consistency fixes ([#111](https://github.com/maverickd650/kubepage-operator/issues/111)) ([dbfa011](https://github.com/maverickd650/kubepage-operator/commit/dbfa0113193ea32227110f7b2a38fbaa921ff294))
* **api:** per-Dashboard shared widget credential defaults ([#114](https://github.com/maverickd650/kubepage-operator/issues/114)) ([b2a27f3](https://github.com/maverickd650/kubepage-operator/commit/b2a27f38a816701ce3f0c2a7ae25d98ea17878af))
* **controller:** harden dashboard Deployment defaults ([#144](https://github.com/maverickd650/kubepage-operator/issues/144)) ([39559ec](https://github.com/maverickd650/kubepage-operator/commit/39559eca1ff5800a537e276ad7cd2e1de58a39e2))
* **controller:** validate widget config keys and surface results in status conditions ([#112](https://github.com/maverickd650/kubepage-operator/issues/112)) ([5868da0](https://github.com/maverickd650/kubepage-operator/commit/5868da0cdd48e866197910ecf00d9ed2cdec8545))
* **dashboard:** discover service cards from Gateway API HTTPRoutes ([#116](https://github.com/maverickd650/kubepage-operator/issues/116)) ([69f6cff](https://github.com/maverickd650/kubepage-operator/commit/69f6cff316faf25abb0ba6423a35b4d714c6a4e0))
* **dashboard:** modernize widget APIs and add 12 new widget types ([#98](https://github.com/maverickd650/kubepage-operator/issues/98)) ([9d11491](https://github.com/maverickd650/kubepage-operator/commit/9d114918e66581e7d5abee5f744ac712a55753be))
* **dashboard:** push-based refresh (SSE + morph swaps) and icon prefix docs ([#122](https://github.com/maverickd650/kubepage-operator/issues/122)) ([079030f](https://github.com/maverickd650/kubepage-operator/commit/079030fae844b523ff9a579db753b07b10de5e73))
* **dashboard:** PWA offline shell and visual regression CI ([#124](https://github.com/maverickd650/kubepage-operator/issues/124)) ([38aaffb](https://github.com/maverickd650/kubepage-operator/commit/38aaffb3dfba1deee20a724e4f992e1bd1565bc7))
* **dashboard:** widget batch 2 — infra & monitoring widgets ([#123](https://github.com/maverickd650/kubepage-operator/issues/123)) ([a93b586](https://github.com/maverickd650/kubepage-operator/commit/a93b58680ec4f052aae5a9f93ca56181de92ec5b))


### Bug Fixes

* **api:** fail-closed auth, isCIDR egress validation, bounded secrets maps ([#148](https://github.com/maverickd650/kubepage-operator/issues/148)) ([dc1f9b0](https://github.com/maverickd650/kubepage-operator/commit/dc1f9b0d9c4cd95b67bb0d9ab813456a186fa3d6))
* **ci:** scope fuzz task to its target and align k8s-compat image tag ([#99](https://github.com/maverickd650/kubepage-operator/issues/99)) ([cfdde30](https://github.com/maverickd650/kubepage-operator/commit/cfdde30ea4577ca8c03448c2fb293cd2e2dfa75b))
* **controller:** harden RBAC cleanup, drift detection, and annotation handling ([#146](https://github.com/maverickd650/kubepage-operator/issues/146)) ([77bb81f](https://github.com/maverickd650/kubepage-operator/commit/77bb81fe618fb4d4cf1d13a07294f02f12e951f3))
* **controller:** scope dashboard RBAC to multi-card ServiceCard widget secrets ([#113](https://github.com/maverickd650/kubepage-operator/issues/113)) ([48ea6c3](https://github.com/maverickd650/kubepage-operator/commit/48ea6c38330bed3589d1e1130a44ffe874ad140e))
* **dashboard:** bound concurrent bcrypt comparisons in basic auth ([#145](https://github.com/maverickd650/kubepage-operator/issues/145)) ([4e792fc](https://github.com/maverickd650/kubepage-operator/commit/4e792fc6abeb78aec17c0b5fd4ae46aaa5e7e482)), closes [#134](https://github.com/maverickd650/kubepage-operator/issues/134)
* **dashboard:** bound SSE subscribers, cache hashes, prune CA clients ([#143](https://github.com/maverickd650/kubepage-operator/issues/143)) ([84a0d14](https://github.com/maverickd650/kubepage-operator/commit/84a0d14e449301a4a7ebe053a23fe3855848ad38))
* **dashboard:** bound SSE write stalls and insecure-TLS client caches ([#147](https://github.com/maverickd650/kubepage-operator/issues/147)) ([7d14d4e](https://github.com/maverickd650/kubepage-operator/commit/7d14d4e09623a5dc4441610045ada2200234cd92))
* **dashboard:** document and fix missing url for longhorn/glances InfoWidgets ([#96](https://github.com/maverickd650/kubepage-operator/issues/96)) ([0ff336a](https://github.com/maverickd650/kubepage-operator/commit/0ff336a18356e1dae0e59fcae778077e9d9f1295))
* **dashboard:** mobile rendering, contrast, and accessibility fixes from design review ([#119](https://github.com/maverickd650/kubepage-operator/issues/119)) ([414fd7f](https://github.com/maverickd650/kubepage-operator/commit/414fd7fd0706df49a8d5f1c9241c8a27da86f2fb))
* **dashboard:** switch grafana widget to admin/stats, matching gethomepage/homepage ([#121](https://github.com/maverickd650/kubepage-operator/issues/121)) ([76b761f](https://github.com/maverickd650/kubepage-operator/commit/76b761fe5f78b9700e9d3f351df246557eadd5e1))
* **dashboard:** widget-review fixes from gethomepage/homepage comparison ([#97](https://github.com/maverickd650/kubepage-operator/issues/97)) ([a79ca17](https://github.com/maverickd650/kubepage-operator/commit/a79ca1734e08cbb9538c32d4dbf797e453d9540a))
* fix design issues ([#91](https://github.com/maverickd650/kubepage-operator/issues/91)) ([c911ecf](https://github.com/maverickd650/kubepage-operator/commit/c911ecf56d72d8217b6d80dd21631596df836d38))


### Code Refactoring

* **api:** remove single-entry inline form from Bookmark/ServiceCard/InfoWidget ([#117](https://github.com/maverickd650/kubepage-operator/issues/117)) ([946fd7b](https://github.com/maverickd650/kubepage-operator/commit/946fd7b311b3bbbd4f5dd1b403242059bd1ae514))

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
