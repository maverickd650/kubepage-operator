# Changelog

## [0.1.0](https://github.com/maverickd650/kubepage-operator/compare/v0.0.1...v0.1.0) (2026-06-22)


### Features

* add features ([afd92fc](https://github.com/maverickd650/kubepage-operator/commit/afd92fc36160abacaba07b5d0c03f5a56c81e711))
* Phase 0 foundations for homepage operator (pin version, shared types, render harness) ([36e3a27](https://github.com/maverickd650/kubepage-operator/commit/36e3a279ae54cf2495bfd086cfc784ee530bc605))
* Phase 1 - render Configuration into an owned ConfigMap (settings.yaml end-to-end) ([6b128f3](https://github.com/maverickd650/kubepage-operator/commit/6b128f38c2e9488cbde06ba8d36488f35a6c2d3d))
* Phase 2 - ServiceEntry CRD with file-based secret delivery ([5d99268](https://github.com/maverickd650/kubepage-operator/commit/5d992685c2c17d933d1e563807f96844a00e5d9e))
* Phase 3 - Bookmark CRD with bookmarks.yaml rendering ([09bd29b](https://github.com/maverickd650/kubepage-operator/commit/09bd29b23a64d9fc13a3691ad2f2448dbb849002))
* Phase 4 - InfoWidget CRD with widgets.yaml rendering ([5faded8](https://github.com/maverickd650/kubepage-operator/commit/5faded812ee2d2286c3abccb47bdfbed164b5cff))
* Phase 5 - exposure, status, Helm packaging, and docs polish ([5107213](https://github.com/maverickd650/kubepage-operator/commit/51072135579c3b4f1a958be4bdb41fd6fb9f432e))
* Phase 6.0 - native dashboard spine (Prometheus widget, htmx card grid) ([46f24ad](https://github.com/maverickd650/kubepage-operator/commit/46f24ad3a34bbcd26e6f85a0ae8a976e92c7642c))
* Phase 6.1 - dashboard look/settings parity, search box, bookmarks ([abf864b](https://github.com/maverickd650/kubepage-operator/commit/abf864bcb088a13e84bd37f08ba8338c6b413388))
* Phase 6.2-6.4 - widget set, UniFi, native-dashboard cutover, Gateway API ([9b47909](https://github.com/maverickd650/kubepage-operator/commit/9b47909f5de8a37d4233f9e1f0c4e7436d59174e))


### Bug Fixes

* **controller:** wire remaining InstanceSpec fields into the Deployment ([85f5763](https://github.com/maverickd650/kubepage-operator/commit/85f5763a2be0552b6b647c76f0d7b015ad9dd379))
* **files:** update files ([111be2b](https://github.com/maverickd650/kubepage-operator/commit/111be2b61a6d6374a699ca825dc2f257abb09d97))
* unbreak image build & lint, add secret-source admission policies ([8834778](https://github.com/maverickd650/kubepage-operator/commit/883477882a4e4836e343e20ca1556c7763d9871c))
* unbreak image build and lint, add secret-source admission policies ([d42d2f9](https://github.com/maverickd650/kubepage-operator/commit/d42d2f919b46179cf0bc6d942d18a25273d53e97))
