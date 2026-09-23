# Changelog

## [0.2.0](https://github.com/sBurmester/recipe-reader/compare/v0.1.4...v0.2.0) (2026-09-23)


### ⚠ BREAKING CHANGES

* **db:** migrate with goose instead of golang-migrate (6/6) ([#62](https://github.com/sBurmester/recipe-reader/issues/62))

### Refactoring

* **db:** migrate with goose instead of golang-migrate (6/6) ([#62](https://github.com/sBurmester/recipe-reader/issues/62)) ([419fd08](https://github.com/sBurmester/recipe-reader/commit/419fd087cf0f6b585195b19e3aef837fda5d472c))

## [0.1.4](https://github.com/sBurmester/recipe-reader/compare/v0.1.3...v0.1.4) (2026-09-23)


### Bug Fixes

* **go:** update module github.com/openai/openai-go to v3 ([8b162b3](https://github.com/sBurmester/recipe-reader/commit/8b162b3984cf8cd8fda3d54a739f8136c1d9c39f))

## [0.1.3](https://github.com/sBurmester/recipe-reader/compare/v0.1.2...v0.1.3) (2026-09-23)


### Bug Fixes

* **docker:** wait for postgres over tcp, not its init socket ([#52](https://github.com/sBurmester/recipe-reader/issues/52)) ([fc47c4f](https://github.com/sBurmester/recipe-reader/commit/fc47c4fd5dc351937cdc1c8d25404e91bf745a95))
* **go:** update go modules ([#56](https://github.com/sBurmester/recipe-reader/issues/56)) ([81f6c91](https://github.com/sBurmester/recipe-reader/commit/81f6c9113c1441b5cb5869f4ac3062eea8afb76c))

## [0.1.2](https://github.com/sBurmester/recipe-reader/compare/v0.1.1...v0.1.2) (2026-09-23)


### CI

* keep dependencies updated with renovate (5/6) ([#45](https://github.com/sBurmester/recipe-reader/issues/45)) ([9b7f32d](https://github.com/sBurmester/recipe-reader/commit/9b7f32dbbd70b6d001e48c8dc98c667f95558912))

## [0.1.1](https://github.com/sBurmester/recipe-reader/compare/v0.1.0...v0.1.1) (2026-09-23)


### Bug Fixes

* **ci:** publish a release only after its assets are attached ([#42](https://github.com/sBurmester/recipe-reader/issues/42)) ([ae55cd6](https://github.com/sBurmester/recipe-reader/commit/ae55cd6a88b49df1978a12b516a317548940da9d))

## 0.1.0 (2026-09-23)


### Features

* **ci:** release automatically from a version tag (4/6) ([#39](https://github.com/sBurmester/recipe-reader/issues/39)) ([076e994](https://github.com/sBurmester/recipe-reader/commit/076e994a4b392b8c95ec8014b6acea099cd8644b))


### Bug Fixes

* **cli:** say what a cancelled migration and a bad command line mean (3/6) ([#38](https://github.com/sBurmester/recipe-reader/issues/38)) ([0a25d5c](https://github.com/sBurmester/recipe-reader/commit/0a25d5c75166c96bca6bc8a8318039a20088c171))
* **compose:** pass .env into the app container (2/6) ([#37](https://github.com/sBurmester/recipe-reader/issues/37)) ([2080259](https://github.com/sBurmester/recipe-reader/commit/20802590b02669a3a082552603eb201b9205d56f))
* **db:** hold the import lock on its own connection (1/6) ([#36](https://github.com/sBurmester/recipe-reader/issues/36)) ([b18f4b7](https://github.com/sBurmester/recipe-reader/commit/b18f4b79b5eb0b166a122fa993bef355585b0551))


### CI

* start the first release at 0.1.0 ([#41](https://github.com/sBurmester/recipe-reader/issues/41)) ([859881c](https://github.com/sBurmester/recipe-reader/commit/859881cd11e593509cc5316d8e2872bc347fbbb5))
