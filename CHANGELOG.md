# Changelog

## [1.22.0](https://github.com/Zxela/claude-monitor/compare/v1.21.2...v1.22.0) (2026-03-27)


### Features

* restore horizontal canvas timeline with improved bar sizing ([71304c6](https://github.com/Zxela/claude-monitor/commit/71304c6a1e16ccf1a11708e8f8ffe2b31a6a476d))

## [1.21.2](https://github.com/Zxela/claude-monitor/compare/v1.21.1...v1.21.2) (2026-03-27)


### Bug Fixes

* security docs, npm vulns, semantic HTML, memory bounds, docker leak ([d6a3445](https://github.com/Zxela/claude-monitor/commit/d6a34454721f0bb5681d1fa79ed1b42afb3c1ad2))

## [1.21.1](https://github.com/Zxela/claude-monitor/compare/v1.21.0...v1.21.1) (2026-03-27)


### Bug Fixes

* WebSocket CORS, search index perf, race conditions, error masking ([9add626](https://github.com/Zxela/claude-monitor/commit/9add626aa681ffc857f3dba52c2971490382769b))

## [1.21.0](https://github.com/Zxela/claude-monitor/compare/v1.20.1...v1.21.0) (2026-03-27)


### Features

* accessibility pass — focus rings, reduced motion, semantic HTML ([9baa262](https://github.com/Zxela/claude-monitor/commit/9baa26270e7c2386fa085fa931768b6a14bbe421))
* collapsible sidebar with toggle button ([5c60a3d](https://github.com/Zxela/claude-monitor/commit/5c60a3da736772f3d70a94b6066b94ab5749e8a8))
* consolidate typography and color system ([3b1ee45](https://github.com/Zxela/claude-monitor/commit/3b1ee4535e9da1fbeb369ad99a34d17120dec7c8))
* differentiate status animations and improve scroll-lock positioning ([1255085](https://github.com/Zxela/claude-monitor/commit/1255085122665e4d9fa1b1f3f17ebc8353e776b1))
* link team agents to team lead session as children ([ba0a65d](https://github.com/Zxela/claude-monitor/commit/ba0a65de03c3f9ac0a46d5b46a56d5d16b02335b))
* production readiness overhaul — security, performance, UX, and architecture ([6887256](https://github.com/Zxela/claude-monitor/commit/688725631501baab656b456e8cc4fd6eb09d1f2c))
* replace force-directed graph with horizontal DAG pipeline view ([fff2377](https://github.com/Zxela/claude-monitor/commit/fff237752351d97a6516e39573339275504d6c62))
* replace horizontal canvas Gantt chart with vertical HTML timeline ([c91c9b7](https://github.com/Zxela/claude-monitor/commit/c91c9b7a271ec26956b8430a0d421f4482bb066c))
* session card progressive disclosure, cost tiers, and breakdown filter ([1e01816](https://github.com/Zxela/claude-monitor/commit/1e01816df9e3190b4f8cc01b2320cf5cd45496fd))


### Bug Fixes

* cost calculation — deduplicate streaming chunks, update pricing, fix replay ([ade45a9](https://github.com/Zxela/claude-monitor/commit/ade45a963edfdffe377dd443ae8cd344a80d0650))

## [1.20.1](https://github.com/Zxela/claude-monitor/compare/v1.20.0...v1.20.1) (2026-03-27)


### Bug Fixes

* expanding one parent doesn't expand all others ([f818289](https://github.com/Zxela/claude-monitor/commit/f818289bb6d52c7e81c725d18d79824801f22ff9))

## [1.20.0](https://github.com/Zxela/claude-monitor/compare/v1.19.0...v1.20.0) (2026-03-26)


### Features

* rename toggle to Minimize all, default subagents collapsed ([90e037f](https://github.com/Zxela/claude-monitor/commit/90e037fea2fce9d8f1fd7b59c58fcaf8ee9f1aa7))

## [1.19.0](https://github.com/Zxela/claude-monitor/compare/v1.18.1...v1.19.0) (2026-03-26)


### Features

* add --broadcast flag as shorthand for --bind 0.0.0.0 ([52b869b](https://github.com/Zxela/claude-monitor/commit/52b869b55bacede756fc0ff9f93f69ecf7f7f90b))

## [1.18.1](https://github.com/Zxela/claude-monitor/compare/v1.18.0...v1.18.1) (2026-03-26)


### Bug Fixes

* XSS in update banner, cycle guard in history view, expand button DOM fix ([b7723e3](https://github.com/Zxela/claude-monitor/commit/b7723e3b8e0f90520f98c0dd2481036513b026b0))

## [1.18.0](https://github.com/Zxela/claude-monitor/compare/v1.17.3...v1.18.0) (2026-03-26)


### Features

* classify Agent tool calls as 'agent' type, add click-to-navigate to subagent ([04af71a](https://github.com/Zxela/claude-monitor/commit/04af71a4e54a6fa39e032f7e04615b9ed53b59ec))

## [1.17.3](https://github.com/Zxela/claude-monitor/compare/v1.17.2...v1.17.3) (2026-03-26)


### Bug Fixes

* remove green border under Active Now section ([a4b583b](https://github.com/Zxela/claude-monitor/commit/a4b583be6aca63118f2341cbc42266fef687cea5))

## [1.17.2](https://github.com/Zxela/claude-monitor/compare/v1.17.1...v1.17.2) (2026-03-26)


### Bug Fixes

* prevent expanded feed entries from overlapping following rows ([e9449c8](https://github.com/Zxela/claude-monitor/commit/e9449c8cbdd9b830a730aae03692021ec3c5b652))

## [1.17.1](https://github.com/Zxela/claude-monitor/compare/v1.17.0...v1.17.1) (2026-03-26)


### Bug Fixes

* recalculate IsActive in Store.All() to prevent stale active sessions ([16523c8](https://github.com/Zxela/claude-monitor/commit/16523c8027df125117d7b34ddebbf3e280e27a24))

## [1.17.0](https://github.com/Zxela/claude-monitor/compare/v1.16.5...v1.17.0) (2026-03-26)


### Features

* add 'migrate' subcommand (status, rollback, up) ([352e820](https://github.com/Zxela/claude-monitor/commit/352e820fc4515c29f7c4d8ee4a9a5d8b7c0a546f))
* add agent sequence view with Graph/Sequence toggle ([5696ba0](https://github.com/Zxela/claude-monitor/commit/5696ba05658075fcd4113f89eb75ee3f2278ff15))
* add internal/update package for GitHub release checking ([90fd598](https://github.com/Zxela/claude-monitor/commit/90fd5981acc6bd5afeb813e506c88da992df34db))
* add Makefile targets for migration management ([c9d4b37](https://github.com/Zxela/claude-monitor/commit/c9d4b3710a6a522fa6721b9804678892f1276c02))
* add migration 002 — parent_id column for subagent grouping ([f411841](https://github.com/Zxela/claude-monitor/commit/f411841e38ad52ba238b05df4b74c4fbe1389370))
* add migration registry with RunUp, RunDown, Status ([60b1d66](https://github.com/Zxela/claude-monitor/commit/60b1d66faa8715ba02c6fc0b5cf8ba729847426d))
* add parentId to HistoryRow and historyShowSubagents state ([30aef8d](https://github.com/Zxela/claude-monitor/commit/30aef8d893d23366544477e5b2356b778f9af0bc))
* check for updates on startup, broadcast via WebSocket ([16d73fa](https://github.com/Zxela/claude-monitor/commit/16d73facce796dfc50470f25f35508cca65e7698))
* group subagent sessions under parents in history view ([836d4c3](https://github.com/Zxela/claude-monitor/commit/836d4c3a8f5e1aa080630410a12f26d7d377e434))
* persist and return parent_id in session history ([7c37074](https://github.com/Zxela/claude-monitor/commit/7c37074ac71e4db2aec5ff18b29f214c09ad6e8c))
* replace CREATE TABLE with migration 1, integrate with store.Open ([b98873b](https://github.com/Zxela/claude-monitor/commit/b98873b76af48a05de3b2c523921ac690ff990f1))
* responsive hamburger menu for topbar on mobile ([8601587](https://github.com/Zxela/claude-monitor/commit/8601587dfc775af310a0d03535c6b1d5e903afcd))
* show dismissable update banner in web UI ([ac48cad](https://github.com/Zxela/claude-monitor/commit/ac48cad15b4dc7e08abfb61a5a6f4f5b99165dc1))
* show token/cache/error stats in compact cards, widen sidebar to 283px ([944040f](https://github.com/Zxela/claude-monitor/commit/944040f9870c89c34b4f646fc6ae7f4d34c0331b))


### Bug Fixes

* add 5s timeout to search handler to prevent DoS ([51c703a](https://github.com/Zxela/claude-monitor/commit/51c703aeb67085d9ef5b834116f9c29e82d4ead6))
* add ARIA labels, roles, and keyboard support for accessibility ([9eb7452](https://github.com/Zxela/claude-monitor/commit/9eb74528c422fed76b4267b649685841e5e597a7))
* add error handling to all API calls in api.ts ([940a784](https://github.com/Zxela/claude-monitor/commit/940a784e1a65394877ecf7f9edd0d070685e3cc6))
* add mutex to prevent race on prevActive/savedToHistory maps ([6ab214e](https://github.com/Zxela/claude-monitor/commit/6ab214ed44b8d814b5bb33e5aae535357e6967ed))
* default bind to 127.0.0.1 with --bind flag ([fb24f45](https://github.com/Zxela/claude-monitor/commit/fb24f458ab29aea40a8634e2309a752d190fe1e7))
* remove WriteTimeout that kills WebSocket and SSE ([249929c](https://github.com/Zxela/claude-monitor/commit/249929ca9d5b358a7b4c8b6ac6f199a53fdb475d))
* show SESSION HISTORY label and REPLAY button for non-active sessions ([7ff4c2e](https://github.com/Zxela/claude-monitor/commit/7ff4c2e8f91231e0c7d6945c2518485509d483c4))
* wrap WebSocket JSON.parse in try/catch ([9cc4b61](https://github.com/Zxela/claude-monitor/commit/9cc4b61b1ec20af09639e5fff17810f793e09764))

## [1.16.5](https://github.com/Zxela/claude-monitor/compare/v1.16.4...v1.16.5) (2026-03-26)


### Bug Fixes

* Haiku model pricing — versioned name wasn't matching pricing table ([ceee253](https://github.com/Zxela/claude-monitor/commit/ceee253f0a13387597721c42bf897dae1b964623))

## [1.16.4](https://github.com/Zxela/claude-monitor/compare/v1.16.3...v1.16.4) (2026-03-26)


### Bug Fixes

* persist history immediately on startup, not after 30s delay ([89db651](https://github.com/Zxela/claude-monitor/commit/89db6510b5c4d69fc82097aa451e598899e2a8aa))

## [1.16.3](https://github.com/Zxela/claude-monitor/compare/v1.16.2...v1.16.3) (2026-03-26)


### Bug Fixes

* history DB now persists all inactive sessions, not just transitions ([553a080](https://github.com/Zxela/claude-monitor/commit/553a080f22a41fc445bcbca4a65e97f4cc2fbc69))

## [1.16.2](https://github.com/Zxela/claude-monitor/compare/v1.16.1...v1.16.2) (2026-03-26)


### Bug Fixes

* expanded feed entries show full content, responsive layout tweaks ([9ac84ea](https://github.com/Zxela/claude-monitor/commit/9ac84ea31b3b90c9394680eab2f4f049fe1e5a5f))

## [1.16.1](https://github.com/Zxela/claude-monitor/compare/v1.16.0...v1.16.1) (2026-03-26)


### Bug Fixes

* remove card slide-in animation — caused flashing on every re-render ([98d2ce8](https://github.com/Zxela/claude-monitor/commit/98d2ce869d2a826ea8a6de357c1b666ec1aba94a))

## [1.16.0](https://github.com/Zxela/claude-monitor/compare/v1.15.0...v1.16.0) (2026-03-26)


### Features

* cost breakdown popover with donut chart, token bars, top sessions ([d831fa1](https://github.com/Zxela/claude-monitor/commit/d831fa1f5cdb0f7fb5be98a6348af4d9d8b1ee59))
* responsive layout for mobile/tablet, first-time onboarding tooltip ([9b55e6e](https://github.com/Zxela/claude-monitor/commit/9b55e6e69a903a86966a52b5d26d3dc3eb179ecf))

## [1.15.0](https://github.com/Zxela/claude-monitor/compare/v1.14.3...v1.15.0) (2026-03-26)


### Features

* CSV export, tool result inheritance, card animations, stat tooltips ([e3a5d79](https://github.com/Zxela/claude-monitor/commit/e3a5d79032dc78a405af2e720d6a278c1ec9df7f))

## [1.14.3](https://github.com/Zxela/claude-monitor/compare/v1.14.2...v1.14.3) (2026-03-26)


### Bug Fixes

* consistent active counts, eliminate render flashing ([83aad96](https://github.com/Zxela/claude-monitor/commit/83aad96f1c4c4cff1d39f45d8e14406b161d6df2))

## [1.14.2](https://github.com/Zxela/claude-monitor/compare/v1.14.1...v1.14.2) (2026-03-26)


### Bug Fixes

* ALL filter toggles off, tool_use/result colors, stop active flashing ([e8bfced](https://github.com/Zxela/claude-monitor/commit/e8bfcedd8ef0b23b721479349118ab4c2cc98a72))

## [1.14.1](https://github.com/Zxela/claude-monitor/compare/v1.14.0...v1.14.1) (2026-03-26)


### Bug Fixes

* sessions leave active tab after 30s, history always re-fetches ([01229ee](https://github.com/Zxela/claude-monitor/commit/01229eeb2c29fb4465632bd1b46dbe4b971405b0))

## [1.14.0](https://github.com/Zxela/claude-monitor/compare/v1.13.2...v1.14.0) (2026-03-26)


### Features

* add toolUseMap in feed-panel and toolUseId fields to ParsedMessage ([5941a82](https://github.com/Zxela/claude-monitor/commit/5941a824d8e7134a29594098c37a536ab86eff96))


### Bug Fixes

* history opens feed not replay, hide idle badges, real-time sidebar ([f093b0f](https://github.com/Zxela/claude-monitor/commit/f093b0f410aea559ea4bc675dbfa9de732d5095d))

## [1.13.2](https://github.com/Zxela/claude-monitor/compare/v1.13.1...v1.13.2) (2026-03-26)


### Bug Fixes

* search API returns [] not null when no results (fixes CI test) ([ef299e5](https://github.com/Zxela/claude-monitor/commit/ef299e5cd10ad6ff9ccad33528ce6a6e44042388))

## [1.13.1](https://github.com/Zxela/claude-monitor/compare/v1.13.0...v1.13.1) (2026-03-26)


### Bug Fixes

* run go mod tidy — fix CI lint failure ([c0b100a](https://github.com/Zxela/claude-monitor/commit/c0b100af972ea4bc66dcdabf5bd1504fd480cbe0))

## [1.13.0](https://github.com/Zxela/claude-monitor/compare/v1.12.2...v1.13.0) (2026-03-26)


### Features

* add Canvas 2D timeline/waterfall view with zoom and pan ([3713608](https://github.com/Zxela/claude-monitor/commit/37136082d3df2c57e99ab96d4de168513c23cb90))
* add keyboard navigation — ↑↓ focus, Enter select, ←→ expand/collapse ([b6a7754](https://github.com/Zxela/claude-monitor/commit/b6a775490be42821de44ecad5228b9480a4247b9))
* add scroll lock button and back-to-feed link ([e2f9169](https://github.com/Zxela/claude-monitor/commit/e2f91691747c08d1a7efab1691f339b4f4740faf))
* browser notifications for budget exceeded and agent errors ([6c6c5aa](https://github.com/Zxela/claude-monitor/commit/6c6c5aa8c8a5e72725776916b7a586ca84dfb234))
* persist session/view in URL hash, restore on load ([72dd24a](https://github.com/Zxela/claude-monitor/commit/72dd24a3c25fc10a11e4aee6774555ff54dce15f))
* replay keyboard controls — Space play/pause, R restart, ←→ step ([ea2a0f5](https://github.com/Zxela/claude-monitor/commit/ea2a0f55f898608b4a6caa3c6e299fdee5d97985))
* show current tool on cards, click error count to filter ([9d10e98](https://github.com/Zxela/claude-monitor/commit/9d10e9806ef71f7379ad6040c5ec88bfcfee5107))
* visual grouping of tool calls with their results ([6adf7c6](https://github.com/Zxela/claude-monitor/commit/6adf7c6b3e6fdab7abd741fb04f307fb91cfe91d))

## [1.13.0](https://github.com/Zxela/claude-monitor/compare/v1.12.1...v1.13.0) (2026-03-26)


### Features

* feed panel shows all sessions by default (multi-session mode) ([62a03de](https://github.com/Zxela/claude-monitor/commit/62a03de175d3bfa0a53ad87a3390d6ca94e3a3f8))


### Bug Fixes

* widen feed timestamp column to fit 12-hour AM/PM format ([d558a3d](https://github.com/Zxela/claude-monitor/commit/d558a3de5e4310ed2d9b6b6658a15b63e9b7e7d5))

## [1.12.1](https://github.com/Zxela/claude-monitor/compare/v1.12.0...v1.12.1) (2026-03-26)


### Bug Fixes

* topbar matches old design — segmented layout, flash animation ([569fa13](https://github.com/Zxela/claude-monitor/commit/569fa137df5fdcec6de959f8716a6894d1dd6e2d))

## [1.12.0](https://github.com/Zxela/claude-monitor/compare/v1.11.0...v1.12.0) (2026-03-26)


### Features

* add OpenAPI spec, comprehensive API tests, --swagger flag ([cd7ef82](https://github.com/Zxela/claude-monitor/commit/cd7ef8247cb911103b249f3e52c1dc9f552a172b))

## [1.11.0](https://github.com/Zxela/claude-monitor/compare/v1.10.7...v1.11.0) (2026-03-26)


### Features

* hide idle subagents from active panel after 5 minutes ([ed2b9af](https://github.com/Zxela/claude-monitor/commit/ed2b9afb11384bea637eabe27d6619b2d3538a0d))
* idle subagent toggle, stop committing build assets ([38a4a38](https://github.com/Zxela/claude-monitor/commit/38a4a38720aadb3f2fb8050a302cfa6ac033ee74))

## [1.10.7](https://github.com/Zxela/claude-monitor/compare/v1.10.6...v1.10.7) (2026-03-26)


### Bug Fixes

* strip [hook:X] and [tool: X] prefixes from content, fix expand ([fc2507b](https://github.com/Zxela/claude-monitor/commit/fc2507b38694c83b313bf46f9ff2bc30293172af))

## [1.10.6](https://github.com/Zxela/claude-monitor/compare/v1.10.5...v1.10.6) (2026-03-26)


### Bug Fixes

* move expand button inline after truncated text ([ee66907](https://github.com/Zxela/claude-monitor/commit/ee66907fe5fbafae87698ab345516242631f0710))

## [1.10.5](https://github.com/Zxela/claude-monitor/compare/v1.10.4...v1.10.5) (2026-03-26)


### Bug Fixes

* feed entry format, scrollbars, expanded code blocks ([31fc169](https://github.com/Zxela/claude-monitor/commit/31fc16975d2c5013950e61692094f6bf265b613e))

## [1.10.4](https://github.com/Zxela/claude-monitor/compare/v1.10.3...v1.10.4) (2026-03-26)


### Bug Fixes

* match old design polish — card accents, feed animations, statusbar ([5a015c5](https://github.com/Zxela/claude-monitor/commit/5a015c559b1f352dfb72be4c6beb98623b3c6a99))

## [1.10.3](https://github.com/Zxela/claude-monitor/compare/v1.10.2...v1.10.3) (2026-03-26)


### Bug Fixes

* sessions panel border-right extends full height when groups collapsed ([f2dd46e](https://github.com/Zxela/claude-monitor/commit/f2dd46e96611af5e2eaa39abb707d6f2fa51cceb))

## [1.10.2](https://github.com/Zxela/claude-monitor/compare/v1.10.1...v1.10.2) (2026-03-26)


### Bug Fixes

* address code review — shared utils, perf, broken chevron ([7a20448](https://github.com/Zxela/claude-monitor/commit/7a20448821fbb8ad24c1edc6f850182175ef3041))

## [1.10.1](https://github.com/Zxela/claude-monitor/compare/v1.10.0...v1.10.1) (2026-03-26)


### Bug Fixes

* restore visual density, Active/Recent/All filter, colored feed pills ([474a3b7](https://github.com/Zxela/claude-monitor/commit/474a3b71ccb04e9cf9745779cd5e3391912e9e37))

## [1.10.0](https://github.com/Zxela/claude-monitor/compare/v1.9.1...v1.10.0) (2026-03-26)


### Features

* start time groups collapsed by default ([88bad34](https://github.com/Zxela/claude-monitor/commit/88bad340e99b27d2163ca2b043375d0e829a33c5))

## [1.9.1](https://github.com/Zxela/claude-monitor/compare/v1.9.0...v1.9.1) (2026-03-26)


### Bug Fixes

* restore old HTML styling, subagent nesting, and collapse state ([4665716](https://github.com/Zxela/claude-monitor/commit/46657164e8ed03b2d6c661440628aeaf957c04ee))

## [1.9.0](https://github.com/Zxela/claude-monitor/compare/v1.8.0...v1.9.0) (2026-03-26)


### Features

* add Makefile for unified build commands ([eb29e80](https://github.com/Zxela/claude-monitor/commit/eb29e809624639e487778c8b8b3639b5da9ab035))

## [1.8.0](https://github.com/Zxela/claude-monitor/compare/v1.7.7...v1.8.0) (2026-03-26)


### Features

* add /api/projects endpoint ([b844d04](https://github.com/Zxela/claude-monitor/commit/b844d0458ee183ea9b2c204143df271ad7e1c2d0))
* add /api/sessions/grouped endpoint with time buckets ([195fdba](https://github.com/Zxela/claude-monitor/commit/195fdbab5e5b4283f761ac29ad7f8eac317ed257))
* add /api/version endpoint ([85fe021](https://github.com/Zxela/claude-monitor/commit/85fe02144ba0dc5601b15a89341f62b8bef6d8dc))
* add budget popover with localStorage persistence ([54b4acb](https://github.com/Zxela/claude-monitor/commit/54b4acb6f73d040b08b1555d9f0ca2a1d6db591e))
* add Canvas 2D force-directed graph view ([787e0ad](https://github.com/Zxela/claude-monitor/commit/787e0ad0bea28c5a952914fad2098a5b41ab2d16))
* add centralized state management module ([4a4a052](https://github.com/Zxela/claude-monitor/commit/4a4a052960869ff6ef5ed16224fcb6b0e6953778))
* add feed entry and replay styles ([654ba68](https://github.com/Zxela/claude-monitor/commit/654ba689b179c0abefbd221b5df2c25b361ab501))
* add feed, replay, and budget state fields ([b1cbd94](https://github.com/Zxela/claude-monitor/commit/b1cbd9414deb735b2257c7e7ffb28621aa4702c6))
* add help overlay with keyboard shortcuts ([143cbc9](https://github.com/Zxela/claude-monitor/commit/143cbc99412b43cadc71c0809cc49ceca3a0e907))
* add history view with sortable columns ([5a7d6a0](https://github.com/Zxela/claude-monitor/commit/5a7d6a00da4cdbc65addc55986a276b95075138d))
* add install script with macOS quarantine fix ([5ac05c0](https://github.com/Zxela/claude-monitor/commit/5ac05c0de3c902acc5994e499f0fc797595ab1f2))
* add live feed panel with type filters ([dea75f4](https://github.com/Zxela/claude-monitor/commit/dea75f43535ebc17d4c259e4bbc6ac71a4883a65))
* add projectName to search results ([fe11ddf](https://github.com/Zxela/claude-monitor/commit/fe11ddf78d34d445448a3ce4e9e5785d400abdeb))
* add replay panel with SSE stream and scrubber ([999b970](https://github.com/Zxela/claude-monitor/commit/999b97053d8e00fcea34098103f398fdf3b62dcd))
* add search component with command-palette dropdown ([8670e54](https://github.com/Zxela/claude-monitor/commit/8670e54d7ff1aa37ad829e1dcaf3811662300cc5))
* add session card component (expanded + compact variants) ([9171808](https://github.com/Zxela/claude-monitor/commit/9171808b9d77504e1ad96db3961f9841517930f5))
* add shared message renderer for feed and replay ([0ea7a11](https://github.com/Zxela/claude-monitor/commit/0ea7a1185e49986512036b12bdc28ec0de86ff7c))
* add sortable table view ([d640f79](https://github.com/Zxela/claude-monitor/commit/d640f798b4ec04564c4afe90b0ba6da5d621ea69))
* add styles for graph, table, history, budget, and help ([4d9eeb7](https://github.com/Zxela/claude-monitor/commit/4d9eeb7db7ce0e6996580c29e86d4c831256a260))
* add time-grouped session list component ([4e04cac](https://github.com/Zxela/claude-monitor/commit/4e04cacb10a2994ad954eca4e8483ee7cc4a19e3))
* add top bar component with stats and search ([37702f8](https://github.com/Zxela/claude-monitor/commit/37702f81ac6bd313c6dcd3dad6e570a8f3f46c34))
* add TypeScript types and API client ([3b76ab4](https://github.com/Zxela/claude-monitor/commit/3b76ab46d2bad6ef53ab3d04a641e56bf74a4e3a))
* add version variable and --version flag ([f334778](https://github.com/Zxela/claude-monitor/commit/f3347785ddab6a3d57c8e93510611f61ee49ec58))
* add WebSocket client with auto-reconnect ([95a5ecd](https://github.com/Zxela/claude-monitor/commit/95a5ecd73f058da6afce3aa7fee0b0f325628bcc))
* initialize Vite + TypeScript frontend scaffold ([02d35ca](https://github.com/Zxela/claude-monitor/commit/02d35cac43997b765465e55cf62274bc3dc2fa2c))
* wire all components into main entry point ([d08f490](https://github.com/Zxela/claude-monitor/commit/d08f490b61dfa644e9b5dde28b3a58a238a59b84))
* wire feed, replay, graph, table, history, budget, help into main ([c4343e4](https://github.com/Zxela/claude-monitor/commit/c4343e4caac97ae2fd196c25c9a8baa9a3915c0e))


### Bug Fixes

* topbar stats not updating on initial load, search dropdown z-index ([12df3fc](https://github.com/Zxela/claude-monitor/commit/12df3fce28797a5769ef1c521133808f39e72314))
* use go-version-file, add CGO_ENABLED=0, compress release assets ([8d7e4f3](https://github.com/Zxela/claude-monitor/commit/8d7e4f3d5361da185216cd5637411f4425af0cda))

## [1.7.7](https://github.com/Zxela/claude-monitor/compare/v1.7.6...v1.7.7) (2026-03-23)


### Bug Fixes

* read subagent names from .meta.json companion files ([ae4fd45](https://github.com/Zxela/claude-monitor/commit/ae4fd453e0bc85caadae02932553cca472a1085a))

## [1.7.6](https://github.com/Zxela/claude-monitor/compare/v1.7.5...v1.7.6) (2026-03-23)


### Bug Fixes

* address code review findings — XSS, race condition, polling, index ([8c26b71](https://github.com/Zxela/claude-monitor/commit/8c26b71894b3604bfbba2dbdcbac5e2300c62ede))

## [1.7.5](https://github.com/Zxela/claude-monitor/compare/v1.7.4...v1.7.5) (2026-03-23)


### Bug Fixes

* use short agent ID for subagent names instead of task prompt ([0ead099](https://github.com/Zxela/claude-monitor/commit/0ead099fc03fa6109b65c636023a2a3899024cf7))

## [1.7.4](https://github.com/Zxela/claude-monitor/compare/v1.7.3...v1.7.4) (2026-03-23)


### Bug Fixes

* separate agent name from task description in feed display ([29dbf43](https://github.com/Zxela/claude-monitor/commit/29dbf43b03f26e8781a4c35bfcfaf7890152066a))

## [1.7.3](https://github.com/Zxela/claude-monitor/compare/v1.7.2...v1.7.3) (2026-03-23)


### Bug Fixes

* derive subagent names from task description instead of "subagents" ([c7e73ca](https://github.com/Zxela/claude-monitor/commit/c7e73ca60f82c055518414a4b51d59365aa78763))

## [1.7.2](https://github.com/Zxela/claude-monitor/compare/v1.7.1...v1.7.2) (2026-03-23)


### Bug Fixes

* remove speculative stuck/outcome detection, add error feed filtering ([55fae30](https://github.com/Zxela/claude-monitor/commit/55fae30b1820e02ec66c29dbf1a1815324d414ae))

## [1.7.1](https://github.com/Zxela/claude-monitor/compare/v1.7.0...v1.7.1) (2026-03-23)


### Bug Fixes

* redesign session cards, highlight errors in feed, improve naming ([7357e8c](https://github.com/Zxela/claude-monitor/commit/7357e8cd65f9272488410b72219908b447a84d78))

## [1.7.0](https://github.com/Zxela/claude-monitor/compare/v1.6.0...v1.7.0) (2026-03-23)


### Features

* batch 6 — history persistence, outcome tracking, notifications, comparison table, task descriptions ([8bd785c](https://github.com/Zxela/claude-monitor/commit/8bd785c5b78bcf64a8b52cc6434b3aea38288a9f))

## [1.6.0](https://github.com/Zxela/claude-monitor/compare/v1.5.0...v1.6.0) (2026-03-23)


### Features

* batch 5 — responsive layout, accessibility, cost velocity metric ([1011580](https://github.com/Zxela/claude-monitor/commit/1011580582a35bbd9fc7dd3f19e0446706b3ad6c))

## [1.5.0](https://github.com/Zxela/claude-monitor/compare/v1.4.0...v1.5.0) (2026-03-23)


### Features

* batch 4 — keyboard navigation, replay controls, search highlighting, filter solo mode ([78c0973](https://github.com/Zxela/claude-monitor/commit/78c09739d5df99bb1942f0e1a587d2eb216b63f0))

## [1.4.0](https://github.com/Zxela/claude-monitor/compare/v1.3.0...v1.4.0) (2026-03-23)


### Features

* batch 3 — stuck agent detection, stop button, health monitoring ([fe1f33b](https://github.com/Zxela/claude-monitor/commit/fe1f33b225142365c0f570d58c2538e08e479582))

## [1.3.0](https://github.com/Zxela/claude-monitor/compare/v1.2.1...v1.3.0) (2026-03-23)


### Features

* batch 1 — visual refresh, URL state, multi-session feed, current tool display ([5bca183](https://github.com/Zxela/claude-monitor/commit/5bca183927decc7e7cbad97148ff4ae54c6842ac))
* batch 2 — error tracking, error badges, feed panel actions, working count ([5f73c2b](https://github.com/Zxela/claude-monitor/commit/5f73c2b8639996d20fa12f72e728b01f7afdc277))

## [1.2.1](https://github.com/Zxela/claude-monitor/compare/v1.2.0...v1.2.1) (2026-03-23)


### Bug Fixes

* add tooltips to top bar stats clarifying what they measure ([3f1a291](https://github.com/Zxela/claude-monitor/commit/3f1a291da2a8749a7ea4feb37a847f272243236a))

## [1.2.0](https://github.com/Zxela/claude-monitor/compare/v1.1.0...v1.2.0) (2026-03-23)


### Features

* --docker auto-discovery of container .claude/projects mounts ([#11](https://github.com/Zxela/claude-monitor/issues/11)) ([535fc8b](https://github.com/Zxela/claude-monitor/commit/535fc8b29f28ce9d5d68d3ddf50e1cb5c44f8534))
* add agent display type, fix tool_use expand, fix tool_result parsing ([e1391a3](https://github.com/Zxela/claude-monitor/commit/e1391a3c0ade0f2b68446a29305f02982a4cea3a))
* add feed grouping for tool calls and parent/child session hierarchy ([3c642a7](https://github.com/Zxela/claude-monitor/commit/3c642a70a6437e639357980e4fa61cb1867b9924))
* add per-model pricing lookup for cost calculation ([0a38777](https://github.com/Zxela/claude-monitor/commit/0a38777d7c18290610a4ca6984015dfee7f9d08a))
* add session status tracking, StopReason extraction, thinking block support, and UI status badges ([164c079](https://github.com/Zxela/claude-monitor/commit/164c079186b34a2db1cd080725fded0583437cce))
* agent dependency graph with force-directed layout ([266be36](https://github.com/Zxela/claude-monitor/commit/266be364f40abd043431b0bda754d3ba6edeaabf))
* auto-enable Docker discovery when socket exists ([5308ba8](https://github.com/Zxela/claude-monitor/commit/5308ba840257931fb4b31939b5c6b0bf24a81781))
* bootstrap session stats from historical JSONL on startup ([5589344](https://github.com/Zxela/claude-monitor/commit/5589344c75c5c83d3fd9f1bdcfdd97f916a36655))
* budget alerts — configurable spend threshold with visual warning ([258c6ca](https://github.com/Zxela/claude-monitor/commit/258c6cab1e8a961f77fd8c04ef4192b1bf81e38d))
* cross-session search — search all session content from the top bar ([d9c5ea3](https://github.com/Zxela/claude-monitor/commit/d9c5ea34bb43677460751c0e38e5353f62d7bcf0))
* expandable feed entries and improved session selection ([adbced2](https://github.com/Zxela/claude-monitor/commit/adbced28e46eea50e7689ec86b52dc1a3b2e25ba))
* extract tool result content instead of showing [tool_result] ([26c15f6](https://github.com/Zxela/claude-monitor/commit/26c15f69a33271d8105f3e0c3f602ed91e2c6b39))
* feed type filters — toggle visibility of user/assistant/tools/results/hooks ([c29c054](https://github.com/Zxela/claude-monitor/commit/c29c054a4bdd1d54e570c0f1e8a950649a600e2c))
* hook events, agent/skill detail, and improved feed ([10d99c8](https://github.com/Zxela/claude-monitor/commit/10d99c8a0e392f4a4b3e80d9381e3dd66fdbe4e4))
* improved top bar stats — active count, total spend, active cost, smarter cost formatting ([5189e33](https://github.com/Zxela/claude-monitor/commit/5189e33d4ffaacc3cce6f73b1e0d02f65dfae882))
* inline pixel sprites in session cards + cost rate per session ([ff0aa6a](https://github.com/Zxela/claude-monitor/commit/ff0aa6a8664c9f79e33673819c17dfc737edee38))
* keyboard shortcuts and help overlay ([d78ca8b](https://github.com/Zxela/claude-monitor/commit/d78ca8b9c77f3187d9ea8ff2d53ae64bddfddbd7))
* load recent messages when selecting a session ([2c1c331](https://github.com/Zxela/claude-monitor/commit/2c1c331a30b4e210c0586648f66fe03c9b4c09fd))
* session filtering (active/recent/all), collapsible subagents, pixel view filter ([848d65f](https://github.com/Zxela/claude-monitor/commit/848d65f197ff21ce29ceb02cbffcecb5d9564266))
* session names, fix subagent linking, reduce WebSocket noise ([d2b66b4](https://github.com/Zxela/claude-monitor/commit/d2b66b4ce05a0f6afbec4275f4f012aed9a1df4e))
* session replay — scrub, play/pause, speed control ([#3](https://github.com/Zxela/claude-monitor/issues/3)) ([e1af1c2](https://github.com/Zxela/claude-monitor/commit/e1af1c20b41034e78891cf5efd9b4af8ff421261))
* session timeline waterfall view ([d947ce4](https://github.com/Zxela/claude-monitor/commit/d947ce4eb606ab8bb42c75407f343b658d332292))
* show tool call details in feed — file paths, commands, patterns ([1e4e60b](https://github.com/Zxela/claude-monitor/commit/1e4e60b3339fceb174283d45605032699a1a5655))


### Bug Fixes

* add padding around inline sprites in session cards ([2c9bf36](https://github.com/Zxela/claude-monitor/commit/2c9bf36f67eccdef2faca0acb33fa900623bded8))
* address code review issues [#6](https://github.com/Zxela/claude-monitor/issues/6)-9 ([#10](https://github.com/Zxela/claude-monitor/issues/10)) ([f7f1216](https://github.com/Zxela/claude-monitor/commit/f7f1216e83d5640db324f96712b0182468d03599))
* click parent session card to expand/collapse subagents ([55f9610](https://github.com/Zxela/claude-monitor/commit/55f9610111fe90864db952f161115d035e17d9ee))
* consolidate release pipeline into single workflow ([2f4b3a0](https://github.com/Zxela/claude-monitor/commit/2f4b3a01abadfd8ec512e09b6fe8731815d5abbf))
* correct cache hit % — separate read and creation tokens ([0550b14](https://github.com/Zxela/claude-monitor/commit/0550b1427c26110f216284ad0c7a4537dff71182))
* correct frontend field mismatches for cache tokens, message content, tool types, and cache hit % ([2017414](https://github.com/Zxela/claude-monitor/commit/2017414c15d510db3c7725e4c48541f0736c132c))
* Docker discovery matches .claude mounts (not just .claude/projects) ([ec24605](https://github.com/Zxela/claude-monitor/commit/ec2460565556d15bdedc09aaaff3eafefc1e2209))
* escape backticks and template literals in feed content ([4070175](https://github.com/Zxela/claude-monitor/commit/40701753f739b32e00a6f93f040623982213fcc5))
* even spacing around inline sprites — centered alignment, uniform padding ([0bf0292](https://github.com/Zxela/claude-monitor/commit/0bf0292420528d2f08445a3943b00d55da6f28a9))
* expandable content visibility and periodic session refresh ([4a9f6fd](https://github.com/Zxela/claude-monitor/commit/4a9f6fd709ada0184cceaa02c71e31eba6026cdb))
* graph view shows only active + recently active agents (2 min window) ([0c6334c](https://github.com/Zxela/claude-monitor/commit/0c6334c82aadad5d65452fb3a41942c750d07659))
* hook display shows event type and tool name ([6b31186](https://github.com/Zxela/claude-monitor/commit/6b31186ccb9134b6c99379d383be202f52b20e5f))
* hook events showing empty content ([51360e7](https://github.com/Zxela/claude-monitor/commit/51360e796996983aa615f2a632bd8ebcb18732c8))
* improve session filtering UX ([76b023d](https://github.com/Zxela/claude-monitor/commit/76b023de3513b3cd9f2864a9ec2bca19bcce8c56))
* include cache creation tokens in cache hit % denominator ([c139fc0](https://github.com/Zxela/claude-monitor/commit/c139fc0f4836ef94473af258c4e89b576cea17c3))
* log skipped lines with byte offset, remove wildcard CORS from SSE ([b51a564](https://github.com/Zxela/claude-monitor/commit/b51a564670b682370a22c36382c49b35c4515b02))
* log skipped lines with byte offset, remove wildcard CORS from SSE ([bd2b234](https://github.com/Zxela/claude-monitor/commit/bd2b234af8caf25965ccca0df698af827cbefd54))
* parse usage from message.usage, compute cost from tokens, fix cache field name ([9a3461b](https://github.com/Zxela/claude-monitor/commit/9a3461b7cf230bf1b8a67d98d716a4a111ef4fc4))
* prevent MessageCount inflation from non-conversation types and streaming chunks ([74ea5f3](https://github.com/Zxela/claude-monitor/commit/74ea5f3c6ab8c37ebd38d29d61f8d4cead0d550c))
* remove duplicate bootstrap check in broadcast loop ([f0ba5a9](https://github.com/Zxela/claude-monitor/commit/f0ba5a9069b1751216c14666aacca971f81f67a5))
* simplify and improve codebase after review ([9a10f99](https://github.com/Zxela/claude-monitor/commit/9a10f99ccb11e1572468bc4d20cc3856a68c0d15))
* store sprite interval ID, pause animation when tab hidden (closes [#9](https://github.com/Zxela/claude-monitor/issues/9)) ([f7f1216](https://github.com/Zxela/claude-monitor/commit/f7f1216e83d5640db324f96712b0182468d03599))
* timeline only shows conversation events, clamps durations ([11a8db1](https://github.com/Zxela/claude-monitor/commit/11a8db15fd916d833e2b0591f3992b9b3fead94d))
* use actual JSONL timestamps for startedAt and lastActive ([279c670](https://github.com/Zxela/claude-monitor/commit/279c6709ddb0992d6464bc307f839033ef3ec97b))
* use merge commit for release PRs (not squash) for Release Please compatibility ([17003f8](https://github.com/Zxela/claude-monitor/commit/17003f8fe633768308fdf65eaa4796768457edce))


### Performance Improvements

* debounce UI renders with requestAnimationFrame ([f5fa41b](https://github.com/Zxela/claude-monitor/commit/f5fa41bed0c15d94906e4ea6da6b3540bca53215))
* debounce UI renders with requestAnimationFrame ([e049e8d](https://github.com/Zxela/claude-monitor/commit/e049e8d6823f84e338fd90cc3ce2a5d102a2f76a))

## [1.1.0](https://github.com/Zxela/claude-monitor/compare/v1.0.3...v1.1.0) (2026-03-22)


### Features

* add agent display type, fix tool_use expand, fix tool_result parsing ([e1391a3](https://github.com/Zxela/claude-monitor/commit/e1391a3c0ade0f2b68446a29305f02982a4cea3a))


### Bug Fixes

* escape backticks and template literals in feed content ([4070175](https://github.com/Zxela/claude-monitor/commit/40701753f739b32e00a6f93f040623982213fcc5))


### Performance Improvements

* debounce UI renders with requestAnimationFrame ([f5fa41b](https://github.com/Zxela/claude-monitor/commit/f5fa41bed0c15d94906e4ea6da6b3540bca53215))

## [1.0.3](https://github.com/Zxela/claude-monitor/compare/v1.0.2...v1.0.3) (2026-03-22)


### Bug Fixes

* consolidate release pipeline into single workflow ([2f4b3a0](https://github.com/Zxela/claude-monitor/commit/2f4b3a01abadfd8ec512e09b6fe8731815d5abbf))

## [1.0.2](https://github.com/Zxela/claude-monitor/compare/v1.0.1...v1.0.2) (2026-03-22)


### Bug Fixes

* use merge commit for release PRs (not squash) for Release Please compatibility ([17003f8](https://github.com/Zxela/claude-monitor/commit/17003f8fe633768308fdf65eaa4796768457edce))

## [1.0.1](https://github.com/Zxela/claude-monitor/compare/v1.0.0...v1.0.1) (2026-03-22)


### Bug Fixes

* correct cache hit % — separate read and creation tokens ([0550b14](https://github.com/Zxela/claude-monitor/commit/0550b1427c26110f216284ad0c7a4537dff71182))

## [1.0.1](https://github.com/Zxela/claude-monitor/compare/v1.0.0...v1.0.1) (2026-03-22)


### Bug Fixes

* correct cache hit % — separate read and creation tokens ([0550b14](https://github.com/Zxela/claude-monitor/commit/0550b1427c26110f216284ad0c7a4537dff71182))
