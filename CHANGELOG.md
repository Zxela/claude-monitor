# Changelog

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
