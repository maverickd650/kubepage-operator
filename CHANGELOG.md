# Changelog

## [0.6.1](https://github.com/maverickd650/kubepage-operator/compare/v0.6.0...v0.6.1) (2026-07-23)


### Bug Fixes

* **dashboard:** read Longhorn node storage from disks field ([#209](https://github.com/maverickd650/kubepage-operator/issues/209)) ([79e7605](https://github.com/maverickd650/kubepage-operator/commit/79e7605aeea3c3d2f9862cddb4497ef34bc5380f))
* **deps:** update dependency kubectl (1.36.2 → 1.36.3) ([#207](https://github.com/maverickd650/kubepage-operator/issues/207)) ([6237f5a](https://github.com/maverickd650/kubepage-operator/commit/6237f5a455e7e27274e5ee302003ecc442ef9fda))

## [0.6.0](https://github.com/maverickd650/kubepage-operator/compare/v0.5.2...v0.6.0) (2026-07-19)


### Features

* **dashboard:** place search bar inline in header, wrap below when no room ([#201](https://github.com/maverickd650/kubepage-operator/issues/201)) ([091b0d1](https://github.com/maverickd650/kubepage-operator/commit/091b0d163abc7fbd0ff5fd221f5e95304ad9de8c))
* **dashboard:** responsive layout, container-query typography, and fluid page chrome ([#198](https://github.com/maverickd650/kubepage-operator/issues/198)) ([de3056d](https://github.com/maverickd650/kubepage-operator/commit/de3056d1148299bd650a79bb83f7bcf85c81042f))


### Bug Fixes

* **dashboard:** keep status pills top-aligned and shrink chip text to fit ([#200](https://github.com/maverickd650/kubepage-operator/issues/200)) ([0547971](https://github.com/maverickd650/kubepage-operator/commit/05479713c739c0d354cc36165bfb26ba5f9764ac))

## [0.5.2](https://github.com/maverickd650/kubepage-operator/compare/v0.5.1...v0.5.2) (2026-07-18)


### Bug Fixes

* **dashboard:** align plex widget fields with upstream ([#196](https://github.com/maverickd650/kubepage-operator/issues/196)) ([082eb87](https://github.com/maverickd650/kubepage-operator/commit/082eb877a7a489244a4fef2d90d174f270d5fa38))

## [0.5.1](https://github.com/maverickd650/kubepage-operator/compare/v0.5.0...v0.5.1) (2026-07-18)


### Bug Fixes

* **dashboard:** align mealie, homeassistant, linkwarden, prometheus, unifi, and truenas widget fields with upstream ([#195](https://github.com/maverickd650/kubepage-operator/issues/195)) ([36b81ab](https://github.com/maverickd650/kubepage-operator/commit/36b81abc96778b9f43b47ce39bb7290f811d54f8))
* **dashboard:** ignore internalUrl in preview mode, cover the gap with sample data ([#192](https://github.com/maverickd650/kubepage-operator/issues/192)) ([2dabe07](https://github.com/maverickd650/kubepage-operator/commit/2dabe07946f4fb0384ec161f1bdb0a6f39b30454))

## [0.5.0](https://github.com/maverickd650/kubepage-operator/compare/v0.4.0...v0.5.0) (2026-07-18)


### ⚠ BREAKING CHANGES

* **api:** rename InfoWidget options to config and drop the options.url compat path ([#182](https://github.com/maverickd650/kubepage-operator/issues/182))
* **api:** one URL per service — widget url inherits href, merge ping/siteMonitor into monitor ([#181](https://github.com/maverickd650/kubepage-operator/issues/181))
* **api:** fold DashboardStyle into Dashboard.spec.style ([#180](https://github.com/maverickd650/kubepage-operator/issues/180))

### Features

* **api:** fold DashboardStyle into Dashboard.spec.style ([#180](https://github.com/maverickd650/kubepage-operator/issues/180)) ([d87059c](https://github.com/maverickd650/kubepage-operator/commit/d87059ce98ae4ec8f680a44e7ff43e6858ab97dd))
* **api:** internalUrl: auto — derive the in-cluster widget URL from app ([#187](https://github.com/maverickd650/kubepage-operator/issues/187)) ([a5f462d](https://github.com/maverickd650/kubepage-operator/commit/a5f462d6bd162cdf37a7b077e0859d8769bf792a))
* **api:** make dashboardRef optional when the namespace has exactly one Dashboard ([#186](https://github.com/maverickd650/kubepage-operator/issues/186)) ([1a33c45](https://github.com/maverickd650/kubepage-operator/commit/1a33c45cf9a5c9ecd57a96263c4e4eb7ea371de1))
* **api:** one URL per service — widget url inherits href, merge ping/siteMonitor into monitor ([#181](https://github.com/maverickd650/kubepage-operator/issues/181)) ([0ca2edd](https://github.com/maverickd650/kubepage-operator/commit/0ca2eddfff0610e0d9d755b04e980765f57abab6))
* **api:** rename InfoWidget options to config and drop the options.url compat path ([#182](https://github.com/maverickd650/kubepage-operator/issues/182)) ([adee5ea](https://github.com/maverickd650/kubepage-operator/commit/adee5ea58b6747b07a30e13ed6590d77471dfbda))
* **api:** widget-level secretRef shorthand for a whole Secret's keys ([#185](https://github.com/maverickd650/kubepage-operator/issues/185)) ([fb852a8](https://github.com/maverickd650/kubepage-operator/commit/fb852a85fbdecf8c77c74a46fcb4ba8e2140bd49))
* **dashboard:** homepage-parity visual refactor with field/highlight vocabulary, card blur, and layout ordering ([#167](https://github.com/maverickd650/kubepage-operator/issues/167)) ([6f2d2fe](https://github.com/maverickd650/kubepage-operator/commit/6f2d2feec2058786e258f6f7ea531e2710fcdc3e))
* **dashboard:** render basic statusStyle as colored status pills ([#169](https://github.com/maverickd650/kubepage-operator/issues/169)) ([0ea45ac](https://github.com/maverickd650/kubepage-operator/commit/0ea45ac67f0e4a6d3a121e53189efe3031f4b3b7))
* **servicecard:** combined HTTP + pod monitor status with namespace/app pod lookup ([#166](https://github.com/maverickd650/kubepage-operator/issues/166)) ([533421a](https://github.com/maverickd650/kubepage-operator/commit/533421ae4cb80c25c8c7e6dd0a7d5b964952bdcc))


### Bug Fixes

* address full-repo review findings (errorDisplay default, monitor staleness, Secret label watch) ([#170](https://github.com/maverickd650/kubepage-operator/issues/170)) ([616bcb1](https://github.com/maverickd650/kubepage-operator/commit/616bcb1b45041bcc15dbb938a3b52af36b9c951a))
* **dashboard:** catch up fragment/header on tab refocus ([#162](https://github.com/maverickd650/kubepage-operator/issues/162)) ([11ba5f4](https://github.com/maverickd650/kubepage-operator/commit/11ba5f4677253da126a70e72b39cd5f2b9d718b0))
* **dashboard:** keep SSE-triggered refreshes morphing instead of replacing the DOM ([#164](https://github.com/maverickd650/kubepage-operator/issues/164)) ([6d491c2](https://github.com/maverickd650/kubepage-operator/commit/6d491c29335c13b3cfdbafc347e2e5ff326d9056))
* **dashboard:** rewrite incoming fragment to client tab/group state before morph swap ([#168](https://github.com/maverickd650/kubepage-operator/issues/168)) ([09f9d46](https://github.com/maverickd650/kubepage-operator/commit/09f9d46f7e28ecc9ac6d88255c71bd0f6ae7fa2c))
* **mise:** per-checkout golangci-lint issue cache ([#190](https://github.com/maverickd650/kubepage-operator/issues/190)) ([04fe09f](https://github.com/maverickd650/kubepage-operator/commit/04fe09f672ef1a1a51f145080226f15907e2611d))
* per-Dashboard pod selectors, CustomCSS escaping, and monitor/poller fixes from codebase review ([#191](https://github.com/maverickd650/kubepage-operator/issues/191)) ([659677f](https://github.com/maverickd650/kubepage-operator/commit/659677f047ae57f18bd336f04e0360cfcb3d2bd4))

## [0.4.0](https://github.com/maverickd650/kubepage-operator/compare/v0.3.1...v0.4.0) (2026-07-13)


### ⚠ BREAKING CHANGES

* **api:** the following fields changed from string enum to bool; existing CRs setting any of them need their values migrated (e.g. "Enabled" -> true, "Disabled"/"Hidden"/"Contained"/"Auto" -> false) before the next apply/update:

### Features

* **api:** convert on/off toggle fields from string enums to bool ([#153](https://github.com/maverickd650/kubepage-operator/issues/153)) ([00d6b1e](https://github.com/maverickd650/kubepage-operator/commit/00d6b1e5f2ab5590ae04b3032447c7c6dffc2ba2))
* **dashboard:** nested service-card groups ([#154](https://github.com/maverickd650/kubepage-operator/issues/154)) ([105560d](https://github.com/maverickd650/kubepage-operator/commit/105560de9f68b165a7205bbfba5be028411a16b6))

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
